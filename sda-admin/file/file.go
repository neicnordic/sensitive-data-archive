package file

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
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

// Ingest triggers the ingestion of a given file
func Ingest(apiURI, token, username, filepath string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {

		return err
	}
	parsedURL.Path = fmt.Sprintf("%s/file/ingest", parsedURL.Path)

	requestBody := RequestBodyFileIngest{
		Filepath: filepath,
		User:     username,
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

// Accession assigns a given file to a given accession ID
func Accession(apiURI, token, username, filepath, accessionID string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {

		return err
	}
	parsedURL.Path = fmt.Sprintf("%s/file/accession", parsedURL.Path)

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
