package main

// The structs defined here are what is exposed on the APIs

type ErrorResponse struct {
	Error string `json:"error"`
}

type c4ghKeyHash struct {
	Hash         string `json:"hash"`
	Description  string `json:"description"`
	CreatedAt    string `json:"createdAt"`
	DeprecatedAt string `json:"deprecatedAt"`
}

type submissionFileInfo struct {
	AccessionID        string `json:"accessionID,omitempty"`
	FileID             string `json:"fileID"`
	InboxPath          string `json:"inboxPath"`
	Status             string `json:"fileStatus"`
	SubmissionFileSize int64  `json:"submissionFileSize,omitempty"`
	CreatedAt          string `json:"createdAt"`
}
type datasetFileInfo struct {
	ID            string      `json:"id"`
	AccessionID   string      `json:"accessionID"`
	Path          string      `json:"path"`
	CreatedAt     string      `json:"createdAt"`
	Size          int64       `json:"size"`
	DecryptedSize int64       `json:"decryptedSize"`
	Checksums     []*checksum `json:"checksums"`
}

type checksum struct {
	Type     string `json:"type"`
	Checksum string `json:"checksum"`
}

type datasetInfo struct {
	DatasetID string `json:"datasetID"`
	Status    string `json:"status"`
	Timestamp string `json:"timeStamp"`
}
