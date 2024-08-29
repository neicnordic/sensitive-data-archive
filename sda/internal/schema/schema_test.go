package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

const schemaPath = "../../schemas"

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func TestDefaultResponse(t *testing.T) {
	msg := []byte("foo")
	err := ValidateJSON("noSchema.json", msg)
	assert.Equal(t, "unknown reference schema", err.Error())
}

func TestValidateJSONDatasetDeprecate(t *testing.T) {
	okMsg := DatasetMapping{
		Type:      "deprecate",
		DatasetID: "EGAD00123456789",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-deprecate.json", schemaPath), msg))

	badMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "ABCD00123456789",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-deprecate.json", schemaPath), msg))
}

func TestValidateJSONDatasetMapping(t *testing.T) {
	okMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "EGAD00123456789",
		AccessionIDs: []string{
			"EGAF12345678901",
			"EGAF12345678902",
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-mapping.json", schemaPath), msg))

	badMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "ABCD00123456789",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-mapping.json", schemaPath), msg))
}

func TestValidateJSONDatasetRelease(t *testing.T) {
	okMsg := DatasetMapping{
		Type:      "release",
		DatasetID: "EGAD00123456789",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-release.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/dataset-release.json", schemaPath), msg))

	badMsg := DatasetMapping{
		Type:      "release",
		DatasetID: "",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/dataset-release.json", schemaPath), msg))
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/dataset-release.json", schemaPath), msg))
}

func TestValidateJSONInboxRemove(t *testing.T) {
	okMsg := InboxRemove{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		Operation: "remove",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-remove.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/inbox-remove.json", schemaPath), msg))

	badMsg := InboxRemove{
		User:      "JohnDoe",
		FilePath:  "/",
		Operation: "remove",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-remove.json", schemaPath), msg))
}

func TestValidateJSONInboxRename(t *testing.T) {
	okMsg := InboxRename{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		OldPath:   "path/to/file",
		Operation: "rename",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-rename.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/inbox-rename.json", schemaPath), msg))

	badMsg := InboxRename{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		OldPath:   "/",
		Operation: "rename",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-rename.json", schemaPath), msg))
}

func TestValidateJSONInboxUpload(t *testing.T) {
	okMsg := InboxUpload{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		Operation: "upload",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-upload.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/inbox-upload.json", schemaPath), msg))

	badMsg := InboxUpload{
		User:      "JohnDoe",
		FilePath:  "/",
		Operation: "upload",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/inbox-upload.json", schemaPath), msg))
}

func TestValidateJSONInfoError(t *testing.T) {
	okMsg := InfoError{
		Error:           "JohnDoe",
		Reason:          "path/to/file",
		OriginalMessage: "upload",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/info-error.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/info-error.json", schemaPath), msg))

	badMsg := InfoError{
		Error:           "JohnDoe",
		Reason:          "",
		OriginalMessage: "OrgMessage",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/info-error.json", schemaPath), msg))
}

func TestValidateJSONIngestionAccessionRequest(t *testing.T) {
	okMsg := IngestionAccessionRequest{
		User:     "JohnDoe",
		FilePath: "path/to file",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-accession-request.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-accession-request.json", schemaPath), msg))

	badMsg := IngestionAccessionRequest{
		User:     "JohnDoe",
		FilePath: "path/to file",
		DecryptedChecksums: []Checksums{
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-accession-request.json", schemaPath), msg))
}

func TestValidateJSONIngestionAccession(t *testing.T) {
	okMsg := IngestionAccession{
		Type:        "accession",
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-accession.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-accession.json", schemaPath), msg))

	badMsg := IngestionAccession{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-accession.json", schemaPath), msg))
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-accession.json", schemaPath), msg))
}

func TestValidateJSONIngestionCompletion(t *testing.T) {
	okMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-completion.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-completion.json", schemaPath), msg))

	badMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "",
		DecryptedChecksums: []Checksums{
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-completion.json", schemaPath), msg))
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-completion.json", schemaPath), msg))
}

func TestValidateJSONIngestionTrigger(t *testing.T) {
	okMsg := IngestionTrigger{
		Type:     "ingest",
		User:     "JohnDoe",
		FilePath: "path/to/file",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-trigger.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-trigger.json", schemaPath), msg))

	badMsg := IngestionTrigger{
		User:     "JohnDoe",
		FilePath: "path/to file",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-trigger.json", schemaPath), msg))
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-trigger.json", schemaPath), msg))
}

func TestValidateJSONIngestionUserError(t *testing.T) {
	okMsg := IngestionUserError{
		User:     "JohnDoe",
		FilePath: "path/to/file",
		Reason:   "false",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-user-error.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-user-error.json", schemaPath), msg))

	badMsg := IngestionUserError{
		User:     "JohnDoe",
		FilePath: "/",
		Reason:   "false",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-user-error.json", schemaPath), msg))
}

func TestValidateJSONIngestionVerification(t *testing.T) {
	okMsg := IngestionVerification{
		User:        "JohnDoe",
		FilePath:    "path/to/file",
		FileID:      "074803cc-718e-4dc4-a48d-a4770aa9f93b",
		ArchivePath: "filename",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
		ReVerify: false,
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-verification.json", schemaPath), msg))
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-verification.json", schemaPath), msg))

	badMsg := IngestionVerification{
		User:        "JohnDoe",
		FilePath:    "path/to/file",
		FileID:      "074803cc-718e-4dc4-a48d-a4770aa9f93b",
		ArchivePath: "filename",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
		ReVerify: false,
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/federated/ingestion-verification.json", schemaPath), msg))
}

// test for isolated schemas

func TestValidateJSONIsloatedDatasetMapping(t *testing.T) {
	okMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "EGAD00123456789",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994121",
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/dataset-mapping.json", schemaPath), msg))

	badMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/dataset-mapping.json", schemaPath), msg))
}

func TestValidateJSONIsloatedIngestionAccession(t *testing.T) {
	okMsg := IngestionAccession{
		Type:        "accession",
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "ABCD00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-accession.json", schemaPath), msg))

	badMsg := IngestionAccession{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-accession.json", schemaPath), msg))
}

func TestValidateJSONIsolatedIngestionCompletion(t *testing.T) {
	okMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "ABCD00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-completion.json", schemaPath), msg))

	badMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/isolated/ingestion-completion.json", schemaPath), msg))
}

func TestValidateJSONBigpictureSyncFile(t *testing.T) {
	okMsg := SyncFileData{
		AccessionID:       "aa-File-v5y9hk-nc2rfd",
		CorrelationID:     "ced759d1-fd19-4671-9d97-c9b102e8072f",
		DecryptedChecksum: "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6",
		FilePath:          "/inbox/subpath/file_01.c4gh",
		User:              "test.user@example.com",
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/bigpicture/sync-file.json", schemaPath), msg))

	badMsg := SyncFileData{
		AccessionID:       "aa-File-v5y9hk-nc2rfd",
		ArchivePath:       "cd532362-e06e-4460-8490-b9ce64b8d9e7",
		CorrelationID:     "1",
		DecryptedChecksum: "",
		FilePath:          "/inbox/subpath/file_01.c4gh",
		User:              "test.user@example.com",
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/bigpicture/sync-file.json", schemaPath), msg))
}

func TestValidateJSONBigpictureMetadtaSync(t *testing.T) {
	okMsg := SyncMetadata{
		DatasetID: "cd532362-e06e-4460-8490-b9ce64b8d9e7",
		Metadata: Metadata{
			Metadata: "foo",
		},
	}

	msg, _ := json.Marshal(okMsg)
	assert.Nil(t, ValidateJSON(fmt.Sprintf("%s/bigpicture/metadata-sync.json", schemaPath), msg))

	badMsg := SyncMetadata{
		DatasetID: "cd532362-e06e-4460-8490-b9ce64b8d9e7",
		Metadata:  nil,
	}

	msg, _ = json.Marshal(badMsg)
	assert.Error(t, ValidateJSON(fmt.Sprintf("%s/bigpicture/metadata-sync.json", schemaPath), msg))
}
