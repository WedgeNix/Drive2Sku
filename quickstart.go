package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"net/http"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// Drive2Sku is the main instance data for the engine
// it holds onto the needed service for the
// reading portion of the core program
//
var service *drive.Service

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
	readDrive()

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
	ctx := context.Background()

	b, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/drive-go-quickstart.json
	config, err := google.ConfigFromJSON(b, drive.DriveMetadataReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client, skuConn := getClientAndSkuTokens(ctx, config)

	service, err = drive.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve drive Client: %v", err)
	}

	/*err = */
	write2Sku(skuConn)
	// if err != nil {
	// 	log.Fatalf("Unable to write to SKUVault: %v", err)
	// }
}

// readDrive actually reads the drive account's
// pending vendors folder and grabs any and all
// files, downloads them, and deletes them
//
func readDrive() {
	// all Pending Vendor parent id files not in the trash
	fl, err := service.Files.List().Q("'0BzaYO4E7QW9VNG5GejI1LUExaGM' in parents and trashed = false").Do()
	// I would like to make vendor id more dynamic, so if we need to change to id we can
	if err != nil {
		log.Fatalf("Unable to retrieve files: %v", err)
	}

	if len(fl.Files) > 0 {
		for _, f := range fl.Files {
			fmt.Printf("%s (%s)\n", f.Name, f.Id)

			// grabs http request for one of the json files
			// r, err := d2s.service.Files.Get(f.Id).Download()
			// if err != nil {
			// 	log.Fatalf("Unable to download file: %v", err)
			// }

			// goroutine forwards json (request) to sku database
			// go write2Sku(r)
		}
	} else {
		fmt.Println("No files found.")
	}
}

// SkuConn contains access tokens for POST calls
//
type SkuConn struct {
	tokens SkuTokens
	client http.Client
}

// write2Sku writes the intercepted json files out
// to SKUVault via its REST api
//
func write2Sku(conn *SkuConn) {
	fmt.Printf("Tenant:%s User:%s\n", conn.tokens.TenantToken, conn.tokens.UserToken)
}
