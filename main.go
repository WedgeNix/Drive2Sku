package main

// this is a test

import (
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"encoding/json"

	"sync"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// Item represents the inner, important information for each sku object
// this exists in the JSON structure
//
type Item struct {
	LocationCode string
	Quantity     int
	Sku          string
	WarehouseID  int
}

// Payload represents the final payload structure sent off
// to SKUVault, given at most 100 objects
//
type Payload struct {
	Items       []Item
	TenantToken string
	UserToken   string
}

const (
	// throttle is SKUVault's throttle limit
	// ten 100-object payloads every minute
	// every 6300 milliseconds, a post is made
	//
	throttle = 6300
)

var (
	// drv is the Google Drive service
	// it references the account after connecting
	//
	drv *drive.Service

	// toks is the SKUVault connection tokens and client
	// it allows use of tenant and user tokens for POST calls
	//
	toks *SkuTokens

	// endCh signifies the end of the program
	// it is done processing everything once the last
	// value is passed through it
	//
	endCh = make(chan bool)

	// plBufCh holds a maximum of 10 payloads stored concurrently
	//
	plBufCh = make(chan Payload, 10)

	// lastPlCh holds the file's last payload (for deletion)
	//
	lastPlCh = make(chan Payload)

	// wg is a wait group that acts like an atomic reference
	// counter but for goroutines and waits for them to all finish
	//
	wg sync.WaitGroup

	// delFCh is a file channel that holds a potential
	// file eventually to be deleted
	//
	delFCh = make(chan drive.File)
)

// main is the entry point into the server program
// first sets up and reads from the drive
// then forwards the json files in their proper
// format out to SKUVault.
// It loops, controlling the flow, timing, and efficiency
// of the server program so it runs on schedule
// in a smart and practical manner
//
func main() {
	initDriveAndVault()

	wg.Add(1)
	go readDrive()

	// wait for everyone to finish their jobs
	go proctor()

	// 10 payloads every minute to SKUVault
	throttleCh := time.Tick(throttle * time.Millisecond)
	for {
		select {
		case <-throttleCh:
			if len(plBufCh) > 0 {
				go writeVault(<-plBufCh)
			} else {
				go writeVault(<-lastPlCh)
			}
		case <-endCh:
			echo("Finished relaying vendor JSONs")
			return
		}
	}
}

// proctor is a blocking check to see when
// all goroutines have been released from
// the wait group
//
func proctor() {
	wg.Wait()
	endCh <- true
}

// init creates an instance of the engine's collective data
// it sets up the dialog between this server and the drive folder
//
func initDriveAndVault() {
	b, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/drive-go-quickstart.json
	config, err := google.ConfigFromJSON(b, drive.DriveScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	// obtain our Google Drive and SKUVault handles
	drv, toks = getClientAndSkuTokens(context.Background(), config)
}

// readPendingVendors actually reads the drive account's
// pending vendors folder and grabs any and all
// files, downloads them, and deletes them
//
func readDrive() {
	defer wg.Done()

	// all Pending Vendor parent id files not in the trash
	fls, err := drv.Files.List(). /*.PageSize(2)*/ Q(`'0BzaYO4E7QW9VeVFVUGZrMUVLSWs' in parents and trashed = false`).Do()
	if err == nil {
		// store the count of files to be processed
		n := len(fls.Files)
		if n > 0 {
			for _, f := range fls.Files {
				echo(fmt.Sprintf("Processing %s (%s)", f.Name, f.Id))

				// one file at a time
				/*wg.Add(1) // this in unsafe at the moment; file deletion relies on sequence
				go*/chunkToPayloads(*f)
			}
		} else {
			fmt.Println("No files found.")
		}
	}
}

// chunkToPayloads downloads a file
// fitting it into 100-chuck payloads
//
func chunkToPayloads(f drive.File) {
	// defer wg.Done()

	// grabs http request for one of the json files
	res, err := drv.Files.Get(f.Id).Download()
	if err != nil {
		log.Fatalf("Unable to download file: %v", err)
	}
	defer res.Body.Close()

	plCap := 100

	// 100-item capacity payload
	pl := Payload{make([]Item, 0, plCap), toks.TenantToken, toks.UserToken}

	i := 0
	// the entire JSON file structure
	vsd := map[string]map[string]Item{}
	// fmt.Println(`[[[ Decode JSON: PRE ]]]`)
	json.NewDecoder(res.Body).Decode(&vsd)
	// fmt.Println(`[[[ Decode JSON: POST ]]]`)
	for _, v := range vsd {
		// fmt.Printf("%s:\n", k)

		for _, iv := range v {
			i++
			// this is one payload item
			// i is the cursor

			// fmt.Printf("\t%s:\n", ik)

			// payload is full
			if len(pl.Items) == cap(pl.Items) {
				// forward payload into buffered channel
				wg.Add(1)
				// this is the last one
				if i == len(v) {
					plBufCh <- pl
				} else {
					lastPlCh <- pl
				}
				// reset payload
				pl = Payload{make([]Item, 0, plCap), pl.TenantToken, pl.UserToken}
			}

			// add item to payload
			pl.Items = append(pl.Items, iv)

			// fmt.Printf("\t\t\"LocationCode\":\"%s\"\n", iv.LocationCode)
			// fmt.Printf("\t\t\"Quantity\":%d\n", iv.Quantity)
			// fmt.Printf("\t\t\"Sku\":\"%s\"\n", iv.Sku)
			// fmt.Printf("\t\t\"WarehouseId\":\"%d\"\n", iv.WarehouseID)
		}

		// payload is partially full
		if len(pl.Items) != 0 {
			// forward payload into buffered channel
			wg.Add(1)
			lastPlCh <- pl
		}
	}

	// the file is finished chunking into payloads;
	// send it forward for deletion
	delFCh <- f

	// fmt.Printf("Tenant:%s User:%s\n", toks.TenantToken, toks.UserToken)
	// fmt.Println(`[[[ Chunk to payloads: END ]]]`)
}

// deleteFile takes in a drive file
// and actually deletes it from the
// Drive account
//
func deleteFile(f drive.File) {
	echo(fmt.Sprintf(`Deleting file "%s" (%s)`, f.Name, f.Id))

	err := drv.Files.Delete(f.Id).Do()
	if err != nil {
		log.Fatalf("Unable to delete file: %v", err)
	}
}

// writeVault writes the intercepted json files out
// to SKUVault via its REST api
//
func writeVault(pl Payload) {
	defer wg.Done()

	res, err := vaultRequest(`inventory/setItemQuantities`, struct2JSON(pl))
	if err != nil {
		log.Fatalf(`Unable to set item quantities in SKUVault: %v`, err)

		// plug back
		plBufCh <- pl
	}
	defer res.Body.Close()

	var errExt string
	if res.StatusCode < 400 {
		errExt = ""
	} else {
		errExt = fmt.Sprintf("; %s", responseStatus(res))
	}

	echo(fmt.Sprintf(`Uploaded payload (%d/%d)%s`, len(pl.Items), cap(pl.Items), errExt))

	// attempt to delete a file if finished
	// chunking into payloads;
	// since we are dealing with one file at a time
	// it is implied that after a payload write it
	// is safe to delete said file since it is clearly
	// sent out. The payloads back to back are not
	// different files
	select {
	case f := <-delFCh: // delete if ready
		deleteFile(f)
	default: // ignore if not ready
	}
}

// ErrorBody matches the structure of
// the SKUVault response body for an error
//
type ErrorBody struct {
	Sku           string
	Code          int
	LocationCode  string
	WarehouseID   int
	ErrorMessages []string
}

// ResponseBody matches the structure of
// the SKUVault general response body
//
type ResponseBody struct {
	Status string
	Errors []ErrorBody
}
