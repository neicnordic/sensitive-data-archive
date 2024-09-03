package file

import (
	"encoding/json"
	"fmt"

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

// FileIngest triggers the ingestion of a given file
func FileIngest(api_uri, token, username, filepath string) error {

	url := api_uri + "/file/ingest"

	requestBody := RequestBodyFileIngest{
		Filepath: filepath,
		User:     username,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON, reason: %v", err)
	}

	_, err = helpers.PostRequest(url, token, jsonBody)

	if err != nil {
		return err
	}
	return nil
}

// FileAccession assigns a given file to a given accession ID
func FileAccession(api_uri, token, username, filepath, accessionID string) error {

	url := api_uri + "/file/accession"

	requestBody := RequestBodyFileAccession{
		AccessionID: accessionID,
		Filepath: filepath,
		User:     username,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON, reason: %v", err)
	}

	_, err = helpers.PostRequest(url, token, jsonBody)

	if err != nil {
		return err
	}
	return nil
}