package main

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

	// wg is a wait group that acts like an atomic reference
	// counter but for goroutines and waits for them to all finish
	//
	wg sync.WaitGroup

	// fCh is a file channel that holds all potential
	// files eventually to be deleted
	//
	fCh = make(chan drive.File)

	// delFCh is a channel that accepts a
	// number for files to delete in the near future
	//
	delFCh = make(chan int)
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
			go writeVault(<-plBufCh)
		case n := <-delFCh:
			deleteFiles(n)
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
		n := len(fls.Files)
		if n > 0 {
			for _, f := range fls.Files {
				echo(fmt.Sprintf("Processing %s (%s)", f.Name, f.Id))

				wg.Add(1)
				go chunkToPayloads(*f)
			}
		} else {
			fmt.Println("No files found.")
		}

		delFCh <- n
	}
}

// chunkToPayloads downloads a file
// fitting it into 100-chuck payloads
//
func chunkToPayloads(f drive.File) {
	defer wg.Done()

	// fmt.Println(`[[[ Chunk to payloads: BEGIN ]]]`)

	// grabs http request for one of the json files
	res, err := drv.Files.Get(f.Id).Download()
	if err != nil {
		log.Fatalf("Unable to download file: %v", err)
	}
	defer res.Body.Close()

	plCap := 100

	// 100-item capacity payload
	pl := Payload{make([]Item, 0, plCap), toks.TenantToken, toks.UserToken}

	// the entire JSON file structure
	vsd := map[string]map[string]Item{}
	// fmt.Println(`[[[ Decode JSON: PRE ]]]`)
	json.NewDecoder(res.Body).Decode(&vsd)
	// fmt.Println(`[[[ Decode JSON: POST ]]]`)
	for _, v := range vsd {
		// fmt.Printf("%s:\n", k)

		for _, iv := range v {
			// this is one payload item

			// fmt.Printf("\t%s:\n", ik)

			// payload is full
			if len(pl.Items) == cap(pl.Items) {
				// fmt.Println(`[[[ Full payload: BEGIN ]]]`)
				// forward payload into buffered channel
				wg.Add(1)
				plBufCh <- pl
				// reset payload
				pl = Payload{make([]Item, 0, plCap), pl.TenantToken, pl.UserToken}
				// fmt.Println(`[[[ Full payload: END ]]]`)
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
			// fmt.Println(`[[[ Full payload: BEGIN ]]]`)
			// forward payload into buffered channel
			wg.Add(1)
			plBufCh <- pl
		}
	}

	fCh <- f

	// fmt.Printf("Tenant:%s User:%s\n", toks.TenantToken, toks.UserToken)
	// fmt.Println(`[[[ Chunk to payloads: END ]]]`)
}

// deleteFile takes in a drive file
// and actually deletes it from the
// Drive account
//
func deleteFiles(n int) {
	// we wait for the channel
	// to empty n-times
	for i := 0; i < n; i++ {
		f := <-fCh
		echo(fmt.Sprintf(`Deleting file "%s" (%s)`, f.Name, f.Id))
		// we are done fully reading; trash it
		err := drv.Files.Delete(f.Id).Do()
		if err != nil {
			log.Fatalf("Unable to delete file: %v", err)
		} else {
			echo(fmt.Sprintf(`"%s" (%s) deleted!`, f.Name, f.Id))
		}
	}
}

// writeVault writes the intercepted json files out
// to SKUVault via its REST api
//
func writeVault(pl Payload) {
	defer wg.Done()

	// fmt.Println(`[[[ Write to SKUVault: BEGIN ]]]`)

	// b, er := ioutil.ReadAll(struct2JSON(pl))
	// if er != nil {
	// 	log.Fatalf(`Unable to read payload: %v`, er)
	// }
	// fmt.Println(string(b))

	res, err := vaultRequest(`inventory/setItemQuantities`, struct2JSON(pl))
	if err != nil {
		log.Fatalf(`Unable to set item quantities in SKUVault: %v`, err)
	}
	defer res.Body.Close()

	var errExt string
	if res.StatusCode < 400 {
		errExt = ""
	} else {
		errExt = fmt.Sprintf("; %s", responseStatus(res))
	}
	echo(fmt.Sprintf("Uploaded payload (%d/%d)%s", len(pl.Items), cap(pl.Items), errExt))
	// fmt.Println(`[[[ Write to SKUVault: END ]]]`)
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
