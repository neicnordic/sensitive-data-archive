package json

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func ValidateJSON(reference string, body []byte) error {
	dest := getStructName(reference)
	if dest == "" {
		return fmt.Errorf("Unknown reference schema")
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	schema, err := compiler.Compile(reference)
	if err != nil {
		return err
	}

	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}

	if err = schema.Validate(v); err != nil {
		return fmt.Errorf("%#v", err)
	}

	return nil
}

func getStructName(path string) interface{} {
	switch strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) {
	case "dataset-mapping":
		return new(DatasetMapping)
	case "inbox-remove":
		return new(InboxRemove)
	case "inbox-rename":
		return new(InboxRename)
	case "inbox-upload":
		return new(InboxUpload)
	case "info-error":
		return new(InfoError)
	case "ingestion-accession":
		return new(IngestionAccession)
	case "ingestion-accession-request":
		return new(IngestionAccessionRequest)
	case "ingestion-completion":
		return new(IngestionCompletion)
	case "ingestion-trigger":
		return new(IngestionTrigger)
	case "ingestion-user-error":
		return new(IngestionUserError)
	case "ingestion-verification":
		return new(IngestionVerification)
	default:
		return ""
	}
}

type Checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type DatasetMapping struct {
	Type         string   `json:"type"`
	DatasetID    string   `json:"dataset_id"`
	AccessionIDs []string `json:"accession_ids"`
}

type InfoError struct {
	Error           string      `json:"error"`
	Reason          string      `json:"reason"`
	OriginalMessage interface{} `json:"original-message"`
}

type InboxRemove struct {
	User      string `json:"user"`
	FilePath  string `json:"filepath"`
	Operation string `json:"operation"`
}

type InboxRename struct {
	User      string `json:"user"`
	FilePath  string `json:"filepath"`
	OldPath   string `json:"oldpath"`
	Operation string `json:"operation"`
}

type InboxUpload struct {
	User      string `json:"user"`
	FilePath  string `json:"filepath"`
	Operation string `json:"operation"`
}

type IngestionAccession struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type IngestionAccessionRequest struct {
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type IngestionCompletion struct {
	User               string      `json:"user,omitempty"`
	FilePath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type IngestionTrigger struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	EncryptedChecksums []Checksums `json:"encrypted_checksums"`
}

type IngestionUserError struct {
	User     string `json:"user"`
	FilePath string `json:"filepath"`
	Reason   string `json:"reason"`
}

type IngestionVerification struct {
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	FileID             int64       `json:"file_id"`
	ArchivePath        string      `json:"archive_path"`
	EncryptedChecksums []Checksums `json:"encrypted_checksums"`
	ReVerify           bool        `json:"re_verify"`
}
