package dataset

import (
	"encoding/json"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

type RequestBodyDataset struct {
	AccessionIDs []string `json:"accession_ids"`
	DatasetID    string   `json:"dataset_id"`
}

// DatasetCreate creates a dataset from a list of accession IDs and the dataset ID.
func DatasetCreate(api_uri, token, datasetID string, accessionIDs []string) error {

	url := api_uri + "/dataset/create"

	requestBody := RequestBodyDataset{
		AccessionIDs: accessionIDs,
		DatasetID:    datasetID,
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

// DatasetRelease releases a dataset for downloading
func DatasetRelease(api_uri, token, datasetID string) error {

	url := api_uri + "/dataset/release/" + datasetID

	jsonBody := []byte("{}")
	_, err := helpers.PostRequest(url, token, jsonBody)

	if err != nil {
		return err
	}
	return nil
}

