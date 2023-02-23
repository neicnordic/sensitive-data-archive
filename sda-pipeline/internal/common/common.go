package common

import (
	"github.com/xeipuuv/gojsonschema"
)

func ValidateJSON(reference string, body []byte) (*gojsonschema.Result, error) {

	schema := gojsonschema.NewReferenceLoader(reference)
	res, err := gojsonschema.Validate(schema, gojsonschema.NewBytesLoader(body))

	return res, err
}

type Checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Archived struct {
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	FileID             int64       `json:"file_id"`
	ArchivePath        string      `json:"archive_path"`
	EncryptedChecksums []Checksums `json:"encrypted_checksums"`
	ReVerify           bool        `json:"re_verify"`
}

type Completed struct {
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type Finalize struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type InfoError struct {
	Error           string      `json:"error"`
	Reason          string      `json:"reason"`
	OriginalMessage interface{} `json:"original-message"`
}
