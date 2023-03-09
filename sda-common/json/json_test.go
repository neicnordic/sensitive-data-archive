package json

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func TestDefaultResponse(t *testing.T) {
	msg := []byte("foo")
	err := ValidateJSON("noSchema.json", msg)
	assert.Equal(t, "Unknown reference schema", err.Error())
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
	err := ValidateJSON("schemas/federated/dataset-mapping.json", msg)
	assert.Nil(t, err)

	badMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "ABCD00123456789",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/dataset-mapping.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONInboxRemove(t *testing.T) {
	okMsg := InboxRemove{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		Operation: "remove",
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/inbox-remove.json", msg)
	assert.Nil(t, err)

	badMsg := InboxRemove{
		User:      "JohnDoe",
		FilePath:  "/",
		Operation: "remove",
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/inbox-remove.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONInboxRename(t *testing.T) {
	okMsg := InboxRename{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		OldPath:   "path/to/file",
		Operation: "rename",
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/inbox-rename.json", msg)
	assert.Nil(t, err)

	badMsg := InboxRename{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		OldPath:   "/",
		Operation: "rename",
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/inbox-rename.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONInboxUpload(t *testing.T) {
	okMsg := InboxUpload{
		User:      "JohnDoe",
		FilePath:  "path/to/file",
		Operation: "upload",
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/inbox-upload.json", msg)
	assert.Nil(t, err)

	badMsg := InboxUpload{
		User:      "JohnDoe",
		FilePath:  "/",
		Operation: "upload",
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/inbox-upload.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONInfoError(t *testing.T) {
	okMsg := InfoError{
		Error:           "JohnDoe",
		Reason:          "path/to/file",
		OriginalMessage: "upload",
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/info-error.json", msg)
	assert.Nil(t, err)

	badMsg := InfoError{
		Error:           "JohnDoe",
		Reason:          "",
		OriginalMessage: "OrgMessage",
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/info-error.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/federated/ingestion-accession-request.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionAccessionRequest{
		User:     "JohnDoe",
		FilePath: "path/to file",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-accession-request.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/federated/ingestion-accession.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionAccession{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "ABCD00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-accession.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/federated/ingestion-completion.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-completion.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONIngestionTrigger(t *testing.T) {
	okMsg := IngestionTrigger{
		Type:     "ingest",
		User:     "JohnDoe",
		FilePath: "path/to/file",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/ingestion-trigger.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionTrigger{
		User:     "JohnDoe",
		FilePath: "path/to file",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-trigger.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONIngestionUserError(t *testing.T) {
	okMsg := IngestionUserError{
		User:     "JohnDoe",
		FilePath: "path/to/file",
		Reason:   "false",
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/ingestion-user-error.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionUserError{
		User:     "JohnDoe",
		FilePath: "/",
		Reason:   "false",
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-user-error.json", msg)
	assert.Error(t, err)
}

func TestValidateJSONIngestionVerification(t *testing.T) {
	okMsg := IngestionVerification{
		User:        "JohnDoe",
		FilePath:    "path/to/file",
		FileID:      123456789,
		ArchivePath: "filename",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
		ReVerify: false,
	}

	msg, _ := json.Marshal(okMsg)
	err := ValidateJSON("schemas/federated/ingestion-verification.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionVerification{
		User:        "JohnDoe",
		FilePath:    "path/to/file",
		FileID:      123456789,
		ArchivePath: "filename",
		EncryptedChecksums: []Checksums{
			{Type: "sha256", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
		ReVerify: false,
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/federated/ingestion-verification.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/isolated/dataset-mapping.json", msg)
	assert.Nil(t, err)

	badMsg := DatasetMapping{
		Type:      "mapping",
		DatasetID: "",
		AccessionIDs: []string{
			"c177c69c-dcc6-4174-8740-919b8f994122",
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/isolated/dataset-mapping.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/isolated/ingestion-accession.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionAccession{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "ABCD00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/isolated/ingestion-accession.json", msg)
	assert.Error(t, err)
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
	err := ValidateJSON("schemas/isolated/ingestion-completion.json", msg)
	assert.Nil(t, err)

	badMsg := IngestionCompletion{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		AccessionID: "",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	err = ValidateJSON("schemas/isolated/ingestion-completion.json", msg)
	assert.Error(t, err)
}
