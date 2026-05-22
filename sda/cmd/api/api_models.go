package main

// The structs defined here are what is exposed on the APIs

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

type datasetInfo struct {
	DatasetID string `json:"datasetID"`
	Status    string `json:"status"`
	Timestamp string `json:"timeStamp"`
}
