package c4ghkeyhash

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/tidwall/pretty"
)

type C4ghPubKey struct {
	PubKey      string `json:"pubkey"`
	Description string `json:"description"`
}

func Add(apiURI, token, filepath, description string) error {
	fileData, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	requestBody := C4ghPubKey{
		PubKey:      base64.StdEncoding.EncodeToString(fileData),
		Description: description,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON, reason: %v", err)
	}

	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "c4gh-keys/add")

	_, err = helpers.PostRequest(parsedURL.String(), token, jsonBody)
	if err != nil {
		return err
	}

	return nil
}

func Deprecate(apiURI, token, hash string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "c4gh-keys/deprecate", hash)

	_, err = helpers.PostRequest(parsedURL.String(), token, []byte(`{}`))
	if err != nil {
		return err
	}

	return nil
}

func List(apiURI, token string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "c4gh-keys/list")

	response, err := helpers.GetResponseBody(parsedURL.String(), token)
	if err != nil {
		return err
	}

	fmt.Print(string(pretty.Pretty(response)))

	return nil
}
