package file

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/tidwall/pretty"
)

type RequestBodyFileIngest struct {
	Filepath string `json:"filepath"`
	User     string `json:"user"`
}

type RequestBodyFileAccession struct {
	AccessionID string `json:"accession_id"`
	Filepath    string `json:"filepath"`
	User        string `json:"user"`
}

// List returns all files
func List(apiURI, token, username string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "users", username, "files")

	response, err := helpers.GetResponseBody(parsedURL.String(), token)
	if err != nil {
		return err
	}

	fmt.Print(string(pretty.Pretty(response)))

	return nil
}

// Ingest triggers the ingestion of a file via the SDA API.
// Depending on the provided fields in ingestInfo:
// - If ingestInfo.Id is empty, it sends a POST request to /file/ingest with a JSON body containing the file path and user.
// - If ingestInfo.Id is set, it sends a POST request to /file/ingest with the fileid as a query parameter and no JSON body.
func Ingest(ingestInfo helpers.IngestFileInfo) error {
	var jsonBody []byte
	parsedURL, err := url.Parse(ingestInfo.Url)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "file/ingest")

	if ingestInfo.Id == "" {
		if err := helpers.CheckValidChars(ingestInfo.Path); err != nil {
			return err
		}
		requestBody := RequestBodyFileIngest{
			Filepath: ingestInfo.Path,
			User:     ingestInfo.User,
		}
		jsonBody, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON, reason: %v", err)
		}
	} else {
		query := parsedURL.Query()
		query.Set("fileid", ingestInfo.Id)
		parsedURL.RawQuery = query.Encode()
		jsonBody = nil
	}

	_, err = helpers.PostRequest(parsedURL.String(), ingestInfo.Token, jsonBody)
	if err != nil {
		return err
	}

	return nil
}

// SetAccession assigns an accession ID to a specified file for a given user
func SetAccession(apiURI, token, username, filepath, accessionID string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "file/accession")

	requestBody := RequestBodyFileAccession{
		AccessionID: accessionID,
		Filepath:    filepath,
		User:        username,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON, reason: %v", err)
	}

	_, err = helpers.PostRequest(parsedURL.String(), token, jsonBody)
	if err != nil {
		return err
	}

	return nil
}
