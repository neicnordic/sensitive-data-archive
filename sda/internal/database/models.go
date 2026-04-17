package database

type FileInfo struct {
	Size              int64
	Path              string
	ArchivedChecksum  string
	DecryptedChecksum string
	DecryptedSize     int64
	UploadedChecksum  string
}

type MappingData struct {
	FileID             string
	User               string
	SubmissionFilePath string
	SubmissionLocation string
}

type SyncData struct {
	User     string
	FilePath string
	Checksum string
}
type ArchiveData struct {
	FilePath string
	Location string
	FileSize int64

	BackupFilePath string
	BackupLocation string
}

type SubmissionFileInfo struct {
	AccessionID        string
	FileID             string
	InboxPath          string
	Status             string
	SubmissionFileSize int64
	CreatedAt          string
}

type DatasetInfo struct {
	DatasetID string
	Status    string
	Timestamp string
}

type FileDetails struct {
	User string
	Path string
}

type C4ghKeyHash struct {
	Hash         string
	Description  string
	CreatedAt    string
	DeprecatedAt string
}

type ReVerificationData struct {
	FileID               string
	ArchiveFilePath      string
	SubmissionFilePath   string
	SubmissionUser       string
	ArchivedCheckSum     string
	ArchivedCheckSumType string
}
