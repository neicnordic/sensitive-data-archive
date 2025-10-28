package dataset

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

type RequestBodyDataset struct {
	AccessionIDs []string `json:"accession_ids"`
	DatasetID    string   `json:"dataset_id"`
	User         string   `json:"user"`
}

type DatasetFileIDs struct {
	FileID      string `json:"fileID"`
	AccessionID string `json:"accessionID"`
}

// Create creates a dataset from a list of accession IDs and a dataset ID.
func Create(apiURI, token, datasetID, username string, accessionIDs []string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "dataset/create")

	requestBody := RequestBodyDataset{
		AccessionIDs: accessionIDs,
		DatasetID:    datasetID,
		User:         username,
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

// Release releases a dataset for downloading
func Release(apiURI, token, datasetID string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "dataset/release") + "/" + datasetID

	_, err = helpers.PostRequest(parsedURL.String(), token, nil)
	if err != nil {
		return err
	}

	return nil
}

// RotateKey rotates the encryption key for all files in a dataset
func RotateKey(apiURI, token, datasetID string) error {
	// First, get all file IDs in the dataset
	filesURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	filesURL.Path = path.Join(filesURL.Path, "dataset", datasetID, "fileids")

	response, err := helpers.GetResponseBody(filesURL.String(), token)
	if err != nil {
		return fmt.Errorf("failed to get dataset file IDs, reason: %v", err)
	}

	var datasetFiles []DatasetFileIDs
	err = json.Unmarshal(response, &datasetFiles)
	if err != nil {
		return fmt.Errorf("failed to unmarshal dataset file IDs response, reason: %v", err)
	}

	if len(datasetFiles) == 0 {
		return fmt.Errorf("no files found for dataset %s", datasetID)
	}

	// Send rotation request for each file using the internal FileID
	for _, file := range datasetFiles {
		rotateURL, err := url.Parse(apiURI)
		if err != nil {
			return fmt.Errorf("failed to parse API URI for file %s, reason: %v", file.FileID, err)
		}
		rotateURL.Path = path.Join(rotateURL.Path, "file", "rotatekey", file.FileID)

		_, err = helpers.PostRequest(rotateURL.String(), token, nil)
		if err != nil {
			return fmt.Errorf("failed to rotate key for file %s (accession: %s), reason: %v", file.FileID, file.AccessionID, err)
		}
	}

	return nil
}
