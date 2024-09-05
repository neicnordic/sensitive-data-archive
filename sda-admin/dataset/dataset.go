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
}

// Create creates a dataset from a list of accession IDs and the dataset ID.
func Create(apiURI, token, datasetID string, accessionIDs []string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "dataset/create")

	requestBody := RequestBodyDataset{
		AccessionIDs: accessionIDs,
		DatasetID:    datasetID,
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
	parsedURL.Path = path.Join(parsedURL.Path, "dataset/release", datasetID)

	jsonBody := []byte("{}")
	_, err = helpers.PostRequest(parsedURL.String(), token, jsonBody)

	if err != nil {
		return err
	}

	return nil
}

