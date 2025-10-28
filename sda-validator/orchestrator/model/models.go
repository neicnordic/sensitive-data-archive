package model

import "time"

type JobPreparationMessage struct {
	ValidationID string
}
type JobMessage struct {
	ValidationID        string
	ValidatorID         string
	ValidationDirectory string
	Files               []*FileInformation
}

type ValidationInformation struct {
	ValidationID     string
	ValidatorIDs     []string
	SubmissionUserID string
	Files            []*FileInformation
}
type FileInformation struct {
	FileID             string
	FilePath           string
	SubmissionFileSize int64
}

type ValidationResult struct {
	ValidationID     string
	ValidatorResults []*ValidatorResult
}
type ValidatorResult struct {
	ValidatorID string
	Result      string
	StartedAt   time.Time
	FinishedAt  time.Time
	Messages    []*Message
	Files       []*FileResult
}
type FileResult struct {
	FilePath string     `json:"path"`
	Result   string     `json:"result"`
	Messages []*Message `json:"messages"`
}

type Message struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

type ValidatorOutput struct {
	Result   string        `json:"result"`
	Files    []*FileResult `json:"files"`
	Messages []*Message    `json:"messages"`
}

type ValidatorInput struct {
	Files  []*FileInput     `json:"files"`
	Paths  []string         `json:"paths"`
	Config *ValidatorConfig `json:"config"`
}
type FileInput struct {
	Path string `json:"path"`
}
type ValidatorConfig struct {
}

type UserFilesResponse struct {
	FileID             string `json:"fileID"`
	InboxPath          string `json:"inboxPath"`
	SubmissionFileSize int64  `json:"submissionFileSize"`
}
