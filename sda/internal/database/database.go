package database

import (
	"context"
)

type Transaction interface {
	// Commit the transaction
	Commit() error
	// Rollback the transaction
	Rollback() error
	functions
}

type Database interface {
	// BeginTransaction starts a database transaction, either commit or rollback needs to be called when done to release resources and close transaction
	BeginTransaction(ctx context.Context) (Transaction, error)
	// Close the database connection
	Close() error
	SchemaVersion() (int, error)
	Ping(ctx context.Context) error

	functions
}

// functions denotes the available database functions
type functions interface {
	// RegisterFile inserts a file in the database with its inbox location, along with a "registered" log
	// event. If the file already exists in the database, the entry is updated, but
	// a new file event is always inserted.
	// If fileId is provided the new files table row will have that id, otherwise a new uuid will be generated
	// If the unique unique_ingested constraint(submission_file_path, archive_file_path, submission_user) already exists
	// and a different fileId is provided, the fileId in the database will NOT be updated.
	RegisterFile(ctx context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error)

	// GetUploadedSubmissionFilePathAndLocation returns the submission file path and location for a given user and fileID
	// for a file which last event was 'uploaded' or
	GetUploadedSubmissionFilePathAndLocation(ctx context.Context, submissionUser, fileID string) (string, string, error)

	// GetFileIDByUserPathAndStatus checks if a file exists in the database for a given user and submission filepath
	// and returns its fileID for the latest specified status
	GetFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error)

	// CheckAccessionIDOwnedByUser checks if the file a accessionID links to belongs to the user
	// Returns true if a file is found by the accessionID and user, false if not found
	CheckAccessionIDOwnedByUser(ctx context.Context, accessionID, user string) (bool, error)

	// UpdateFileEventLog updates the status in of the file in the files table
	// The message parameter is the rabbitmq message sent on file upload.
	UpdateFileEventLog(ctx context.Context, fileID, event, user, details, message string) error

	// StoreHeader stores the file header in the database
	StoreHeader(ctx context.Context, header []byte, id string) error

	// RotateHeaderKey updates the file header in the database
	RotateHeaderKey(ctx context.Context, header []byte, keyHash, fileID string) error

	// SetArchived marks the file as 'ARCHIVED' with its archive location
	SetArchived(ctx context.Context, location string, file *FileInfo, fileID string) error

	// CancelFile cancels the file and all actions that have been taken (eg, setting checksums, archiving, etc)
	CancelFile(ctx context.Context, fileID string, message string) error

	// IsFileInDataset checks if a file has been added to a dataset
	IsFileInDataset(ctx context.Context, fileID string) (bool, error)

	// GetFileStatus get the latest event for a file id
	GetFileStatus(ctx context.Context, fileID string) (string, error)

	// GetHeader retrieves the file header
	GetHeader(ctx context.Context, fileID string) ([]byte, error)

	// BackupHeader takes a backup of the current encryption header before it is rotated.
	// It stores the fileID, the hex-encoded header, and the current timestamp in a backup table.
	BackupHeader(ctx context.Context, fileID string, header []byte, keyHash string) error

	// SetVerified sets the file decrypted file size and ARCHIVED and UNENCRYPTED checksums
	SetVerified(ctx context.Context, file *FileInfo, fileID string) error

	// GetArchived retrieves the location and size of archive
	GetArchived(ctx context.Context, fileID string) (*ArchiveData, error)

	// CheckAccessionIDExists validates if an accessionID exists in the db
	CheckAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error)

	// SetAccessionID adds a stable id to a file
	// identified by the user submitting it, inbox path and decrypted checksum
	SetAccessionID(ctx context.Context, accessionID, fileID string) error

	// GetAccessionID returns the stable id of a file identified by its file_id
	GetAccessionID(ctx context.Context, fileID string) (string, error)

	// MapFileToDataset maps a file to a dataset in the database
	MapFileToDataset(ctx context.Context, datasetID, fileID string) error

	// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
	GetInboxPath(ctx context.Context, accessionID string) (string, error)

	// UpdateDatasetEvent marks the files in a dataset as "registered","released" or "deprecated"
	UpdateDatasetEvent(ctx context.Context, datasetID, status, message string) error

	// GetFileInfo returns info on a ingested file
	GetFileInfo(ctx context.Context, id string) (*FileInfo, error)

	// GetSubmissionLocation returns the submission location for a file id
	GetSubmissionLocation(ctx context.Context, fileID string) (string, error)

	// GetHeaderByAccessionID retrieves the file header by using stable id
	GetHeaderByAccessionID(ctx context.Context, accessionID string) ([]byte, error)

	// GetMappingData retrieves the file information needed for mapping
	GetMappingData(ctx context.Context, accessionID string) (*MappingData, error)

	// GetSyncData retrieves the file information needed to sync a dataset
	GetSyncData(ctx context.Context, accessionID string) (*SyncData, error)

	// GetFileIDInInbox gets the file id of a file which last known event is either 'registered', 'uploaded', or 'disabled'
	// as that means that ingestion has not been triggered and users are allowed continue uploading or reupload the file to the inbox
	// if no row is found does not return sql.ErrNoRows, just empty string in id return field
	GetFileIDInInbox(ctx context.Context, submissionUser, filePath string) (string, error)

	// CheckIfDatasetExists checks if a dataset already is registered
	CheckIfDatasetExists(ctx context.Context, datasetID string) (bool, error)

	// GetArchivePathAndLocation retrieves the archive_file_path and archive_location for a file with a given accessionID
	GetArchivePathAndLocation(ctx context.Context, accessionID string) (string, string, error)

	// GetArchiveLocation returns the archive location for a file ID
	GetArchiveLocation(ctx context.Context, fileID string) (string, error)

	// SetSubmissionFileSize sets the submission file size for a file
	SetSubmissionFileSize(ctx context.Context, fileID string, submissionFileSize int64) error

	// GetUserFiles retrieves all the files a user submitted
	GetUserFiles(ctx context.Context, userID, pathPrefix string, allData bool) ([]*SubmissionFileInfo, error)

	// ListActiveUsers list all users with files not yet assigned to a dataset
	ListActiveUsers(ctx context.Context) ([]string, error)

	// GetDatasetStatus returns the latest event for a dataset ID
	GetDatasetStatus(ctx context.Context, datasetID string) (string, error)

	// AddKeyHash inserts a new key hash with description to the database
	AddKeyHash(ctx context.Context, keyHash, keyDescription string) error

	// GetKeyHash wraps getKeyHash with exponential stand-off retries
	GetKeyHash(ctx context.Context, fileID string) (string, error)

	// SetKeyHash sets the key hash used to encrypt a file
	SetKeyHash(ctx context.Context, keyHash, fileID string) error

	// ListKeyHashes lists the hashes from the encryption_keys table
	ListKeyHashes(ctx context.Context) ([]*C4ghKeyHash, error)

	// DeprecateKeyHash sets a key hash as deprecated
	DeprecateKeyHash(ctx context.Context, keyHash string) error

	// ListDatasets lists all datasets, their latest event and timestamp
	ListDatasets(ctx context.Context) ([]*DatasetInfo, error)

	// ListUserDatasets lists all datasets, their latest event and timestamp created by a specifc user
	ListUserDatasets(ctx context.Context, submissionUser string) ([]*DatasetInfo, error)

	// UpdateUserInfo upserts user info
	UpdateUserInfo(ctx context.Context, userID, name, email string, groups []string) error

	// GetReVerificationData gets the data to verify a file ingestion by the accessionID
	GetReVerificationData(ctx context.Context, accessionID string) (*ReVerificationData, error)

	// GetReVerificationDataFromFileID gets the data to verify a file ingestion by the fileID
	GetReVerificationDataFromFileID(ctx context.Context, fileID string) (*ReVerificationData, error)

	// GetDecryptedChecksum gets the UNENCRYPTED checksum for a file ID
	GetDecryptedChecksum(ctx context.Context, fileID string) (string, error)

	// GetDatasetFiles returns all file accessionIDs in a dataset
	GetDatasetFiles(ctx context.Context, datasetID string) ([]string, error)

	// GetDatasetFileIDs returns all file IDs in a dataset
	GetDatasetFileIDs(ctx context.Context, datasetID string) ([]string, error)

	// GetFileDetails retrieves user, path and correlation id by giving the file id
	GetFileDetails(ctx context.Context, fileID, event string) (*FileDetails, error)

	// GetSizeAndObjectCountOfLocation Sums the size and count of the files in a location
	GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error)

	// SetBackedUp sets the file backup_path and backup_location
	SetBackedUp(ctx context.Context, location, path, fileID string) error
}
