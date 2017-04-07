package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"net/http"

	"encoding/json"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// drv is the Google Drive service
// it references the account after connecting
//
var drv *drive.Service

// sku is the SKUVault connection tokens and client
// it allows use of tenant and user tokens for POST calls
//
var sku *SkuConn

// main is the entry point into the server program
// first sets up and reads from the drive
// then forwards the json files in their proper
// format out to SKUVault.
// It loops, controlling the flow, timing, and efficiency
// of the server program so it runs on schedule
// in a smart and practical manner
//
func main() {
	// for {
	drive2Sku()

	// TODO: uncomment line below when the program is
	// | | | ready to run on own
	// v v v
	// time.Sleep(24 * time.Hour)
	// }
}

// drive2Sku creates an instance of the engine's collective data
// it sets up the dialog between this server and the drive folder
//
func drive2Sku() {
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
	drv, sku = getClientAndSkuTokens(context.Background(), config)

	readDrive()
}

// readDrive actually reads the drive account's
// pending vendors folder and grabs any and all
// files, downloads them, and deletes them
//
func readDrive() {
	// all Pending Vendor parent id files not in the trash
	fl, err := drv.Files.List().PageSize(1).Q("'0BzaYO4E7QW9VNG5GejI1LUExaGM' in parents and trashed = false").Do()
	// I would like to make vendor id more dynamic, so if we need to change to id we can
	if err != nil {
		log.Fatalf("Unable to retrieve files: %v", err)
	}

	if len(fl.Files) > 0 {
		for _, f := range fl.Files {
			fmt.Printf("%s (%s)\n", f.Name, f.Id)

			// goroutine forwards json (request) to sku database
			write2Sku(*f)
		}
	} else {
		fmt.Println("No files found.")
	}
}

// SkuConn contains access tokens for POST calls
// t represents the POST tenant and user tokens
// c represents the POST http client
//
type SkuConn struct {
	t SkuTokens
	c http.Client
}

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

// write2Sku writes the intercepted json files out
// to SKUVault via its REST api
//
func write2Sku(f drive.File) {
	// grabs http request for one of the json files
	res, err := drv.Files.Get(f.Id).Download()
	if err != nil {
		log.Fatalf("Unable to download file: %v", err)
	}

	// 100-item capacity payload
	pyld := Payload{make([]Item, 0, 100), sku.t.TenantToken, sku.t.UserToken}

	// the entire JSON file structure
	vsd := map[string]map[string]Item{}
	json.NewDecoder(res.Body).Decode(&vsd)
	for k, v := range vsd {
		fmt.Printf("%s:\n", k)

		// this is one payload item
		for ik, iv := range v {
			fmt.Printf("\t%s:\n", ik)

			// payload is full
			if len(pyld.Items) == cap(pyld.Items) {
				// send payload
				sku.Request("setQuantities", &pyld)

				// empty out payload items
				pyld.Items = make([]Item, 0, 100)
			}

			pyld.Items = append(pyld.Items, iv)

			// fmt.Printf("\t\t\"LocationCode\":\"%s\"\n", iv.LocationCode)
			// fmt.Printf("\t\t\"Quantity\":%d\n", iv.Quantity)
			// fmt.Printf("\t\t\"Sku\":\"%s\"\n", iv.Sku)
			// fmt.Printf("\t\t\"WarehouseId\":\"%d\"\n", iv.WarehouseID)
		}
	}

	fmt.Printf("Tenant:%s User:%s\n", sku.t.TenantToken, sku.t.UserToken)
}

// Request sends a POST request using a SKU connection
// it attempts to send a payload
//
func (sc *SkuConn) Request(fn string, p *Payload) {
	req, err := http.NewRequest("POST", "https://app.skuvault.com/api/"+fn, struct2JSON(*p))
	if err != nil {
		log.Fatalf("Unable to obtain SKUVault request: %v", err)
	}
	req.Header.Add("accept", "application/json")
	req.Header.Add("content-type", "application/json")

	// http client based on request initialized
	res, err := sc.c.Do(req)
	if err != nil {
		log.Fatalf("Unable to get SKUVault response: %v", err)
	}
	defer res.Body.Close()
}
