package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"google.golang.org/api/drive/v3"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

// getClientAndSkuTokens uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClientAndSkuTokens(ctx context.Context, config *oauth2.Config) (*drive.Service, *SkuTokens) {
	cacheDriveFile, cacheSkuFile, err := tokenCacheFiles()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential files. %v", err)
	}

	// drive token
	tok, err := oTokenFromFile(cacheDriveFile)
	if err != nil {
		tok = getOTokenFromWeb(config)
		saveOToken(cacheDriveFile, tok)
	}

	// skuvault token
	toks, err := tokensFromFile(cacheSkuFile)
	if err != nil {
		toks = getTokensFromWeb()
		saveTokens(cacheSkuFile, toks)
	}

	drv, err = drive.New(config.Client(ctx, tok))
	if err != nil {
		log.Fatalf("Unable to retrieve drive Service: %v", err)
	}

	return drv, toks
}

// getOTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getOTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// tokenCacheFiles generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFiles() (string, string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", "", err
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
			url.QueryEscape("drive-go-quickstart.json")),
		filepath.Join(tokenCacheDir,
			url.QueryEscape("skuvault-toks.json")), err
}

// oTokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func oTokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// SkuTokens holds
type SkuTokens struct {
	TenantToken string
	UserToken   string
}

// tokensFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokensFromFile(file string) (*SkuTokens, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &SkuTokens{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveOToken uses a file path to create a file and store the
// token in it.
func saveOToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving Drive credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// saveTokens uses a file path to create a file and store the
// token in it.
func saveTokens(file string, toks *SkuTokens) {
	fmt.Printf("Saving SkuVault credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache sku tokens: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(toks)
}

// readJSON, using a file name and a structure,
// populates it with an existing JSON file.
func readJSON(name string, v interface{}) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

// struct2JSON converts a data structure
// in type format into a JSON-reader
func struct2JSON(v interface{}) *strings.Reader {
	// convert interface to JSON bytes
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("Unable to convert interface to reader: %v", err)
	}

	// return reader of stringified JSON bytes
	return strings.NewReader(string(b))
}

// vaultRequest asks SKUVault of the passed in function,
// supplying a reader on a JSON string
func vaultRequest(fn string, jsn *strings.Reader) (*http.Response, error) {
	// get official POST request from SKUVault
	req, err := http.NewRequest("POST", "https://app.skuvault.com/api/"+fn, jsn)
	if err != nil {
		log.Fatalf("Unable to obtain SKUVault request: %v", err)
	}
	req.Header.Add("accept", "application/json")
	req.Header.Add("content-type", "application/json")

	// http client based on request initialized
	client := &http.Client{}
	return client.Do(req)
}

// getSkuCredentials gets the tokens needed for SKU vault
// api calls.
func getTokensFromWeb() *SkuTokens {
	//  Asking for email for SKU Vault account
	// fmt.Printf("SKU Vault email and password: ")
	// fmt.Printf("Enter your SKU Valut Email address.\n")
	// var usrEmail string
	// var pass string
	// _, err := fmt.Scanf("%s %s\n", &usrEmail, &pass)
	// if err != nil {
	// 	log.Fatalf("Unable to read email or password. %v", err)
	// }

	//  Asking for password for SKU Vault account
	// fmt.Printf("Enter your SKU Valut Password.\n")
	// if _, err := fmt.Scan(&pass); err != nil {
	// 	log.Fatalf("Unable to read password %v", err)
	// }

	type Login struct {
		Email    string
		Password string
	}

	// getting SKUVault account login JSON file path
	usr, err := user.Current()
	if err != nil {
		log.Fatalf("Unable to set as user (OS): %v", err)
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)

	// getting SKUVault account login file
	f, err := os.Open(filepath.Join(tokenCacheDir, url.QueryEscape("skuvault-acc.json")))
	if err != nil {
		log.Fatalf("Unable to open SKUVault account file: %v", err)
	}
	defer f.Close()

	lgn := Login{}
	err = json.NewDecoder(f).Decode(&lgn)
	if err != nil {
		log.Fatalf("Unable to decode skuvault-acc.json: %v", err)
	}

	res, err := vaultRequest(`getTokens`, struct2JSON(lgn))
	if err != nil {
		log.Fatalf("Unable to get SKUVault tokens: %v", err)
	}
	defer res.Body.Close()

	// grab the SKUVault account POST tokens
	toks := &SkuTokens{}
	err = json.NewDecoder(res.Body).Decode(toks)
	if err != nil {
		log.Fatalf("Unable to decode SKUVault tokens {\n\t%s,\n\t%s\n}\n: %v", toks.TenantToken, toks.UserToken, err)
	}

	return toks
}

func printResponse(res *http.Response) {
	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalf(`Unable to read SKUVault response body: %v`, err)
	}
	fmt.Println(string(b))
}

// responseStatus
func responseStatus(res *http.Response) string {
	body := ResponseBody{}
	json.NewDecoder(res.Body).Decode(&body)
	defer res.Body.Close()
	return strings.Join(body.Errors[0].ErrorMessages[:], `, `)
}

// echo center-formats messages in a specific style,
// only for the console though.
func echo(s string) {
	L := `[[[`
	R := `]]]`
	IP := 120 - len(L) - len(R)
	LP := IP/2 - len(s)/2
	RP := IP - len(s) - LP

	fmt.Printf("%s%*s%s%*s%s\n", L, LP, ``, s, RP, ``, R)
}
