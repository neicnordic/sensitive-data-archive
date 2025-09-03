// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
)

// RegisterFile inserts a file in the database, along with a "registered" log
// event. If the file already exists in the database, the entry is updated, but
// a new file event is always inserted.
func (dbs *SDAdb) RegisterFile(ctx context.Context, uploadPath, uploadUser string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.RegisterFile")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return "", errors.New("database schema v4 required for RegisterFile()")
	}

	query := "SELECT sda.register_file($1, $2);"

	var fileID string

	err := dbs.DB.QueryRowContext(ctx, query, uploadPath, uploadUser).Scan(&fileID)

	return fileID, err
}

func (dbs *SDAdb) GetFileID(ctx context.Context, corrID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetFileID")
	defer span.End()

	var (
		err   error
		count int
		ID    string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		ID, err = dbs.getFileID(ctx, corrID)
		count++
	}

	return ID, err
}
func (dbs *SDAdb) getFileID(ctx context.Context, corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT DISTINCT file_id FROM sda.file_event_log where correlation_id = $1;"

	var fileID string
	err := db.QueryRowContext(ctx, getFileID, corrID).Scan(&fileID)
	if err != nil {
		return "", err
	}

	return fileID, nil
}

// GetInboxFilePathFromID checks if a file exists in the database for a given user and fileID
// and that is not yet archived
func (dbs *SDAdb) GetInboxFilePathFromID(ctx context.Context, submissionUser, fileID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetInboxFilePathFromID")
	defer span.End()

	var (
		err      error
		count    int
		filePath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		filePath, err = dbs.getInboxFilePathFromID(ctx, submissionUser, fileID)
		count++
	}

	return filePath, err
}

func (dbs *SDAdb) getInboxFilePathFromID(ctx context.Context, submissionUser, fileID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const getFilePath = `SELECT submission_file_path from sda.files where
submission_user= $1 and id = $2
AND EXISTS (SELECT 1 FROM
(SELECT event from sda.file_event_log where file_id = $2 order by started_at desc limit 1)
as subquery WHERE event = 'uploaded');`

	var filePath string
	err := db.QueryRowContext(ctx, getFilePath, submissionUser, fileID).Scan(&filePath)
	if err != nil {
		return "", err
	}

	return filePath, nil
}

// GetFileIDByUserPathAndStatus checks if a file exists in the database for a given user and submission filepath
// and returns its fileID for the latest specified status
func (dbs *SDAdb) GetFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetFileIDByUserPathAndStatus")
	defer span.End()

	var (
		err    error
		fileID string
	)
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		fileID, err = dbs.getFileIDByUserPathAndStatus(ctx, submissionUser, filePath, status)
		if err == nil || strings.Contains(err.Error(), "sql: no rows in result set") {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return fileID, err
}

func (dbs *SDAdb) getFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const getFileID = `SELECT id from sda.files
WHERE submission_user=$1 and submission_file_path =$2 and stable_id IS null
AND EXISTS (SELECT 1 FROM
(SELECT event from sda.file_event_log JOIN sda.files ON sda.files.id=sda.file_event_log.file_id
WHERE submission_user=$1 and submission_file_path =$2 order by started_at desc limit 1)
AS subquery WHERE event = $3);`

	var fileID string
	err := db.QueryRowContext(ctx, getFileID, submissionUser, filePath, status).Scan(&fileID)
	if err != nil {
		return "", err
	}

	return fileID, nil
}

// UpdateFileEventLog updates the status in of the file in the database.
// The message parameter is the rabbitmq message sent on file upload.
func (dbs *SDAdb) UpdateFileEventLog(ctx context.Context, fileUUID, event, corrID, user, details, message string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.UpdateFileEventLog")
	defer span.End()

	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.updateFileEventLog(ctx, fileUUID, event, corrID, user, details, message)
		count++
	}

	return err
}
func (dbs *SDAdb) updateFileEventLog(ctx context.Context, fileUUID, event, corrID, user, details, message string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, details, message) VALUES($1, $2, $3, $4, $5, $6);"

	result, err := db.ExecContext(ctx, query, fileUUID, event, corrID, user, details, message)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// StoreHeader stores the file header in the database
func (dbs *SDAdb) StoreHeader(ctx context.Context, header []byte, id string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.StoreHeader")
	defer span.End()

	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.storeHeader(ctx, header, id)
		count++
	}

	return err
}
func (dbs *SDAdb) storeHeader(ctx context.Context, header []byte, id string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "UPDATE sda.files SET header = $1 WHERE id = $2;"
	result, err := db.ExecContext(ctx, query, hex.EncodeToString(header), id)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// SetArchived marks the file as 'ARCHIVED'
func (dbs *SDAdb) SetArchived(ctx context.Context, file FileInfo, fileID string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.SetArchived")
	defer span.End()

	var err error

	for count := 1; count <= RetryTimes; count++ {
		err = dbs.setArchived(ctx, file, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) setArchived(ctx context.Context, file FileInfo, fileID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const setArchived = "UPDATE sda.files SET archive_file_path = $1, archive_file_size = $2 WHERE id = $3;"
	if _, err := db.ExecContext(ctx, setArchived, file.Path, file.Size, fileID); err != nil {
		return fmt.Errorf("setArchived error: %s", err.Error())
	}

	log.Debugf("checksum: %s", file.UploadedChecksum)
	const addChecksum = `INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('UPLOADED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;`

	if _, err := db.ExecContext(ctx, addChecksum, fileID, file.UploadedChecksum, "SHA256"); err != nil {
		return fmt.Errorf("addChecksum error: %s", err.Error())
	}

	return nil
}

func (dbs *SDAdb) GetFileStatus(ctx context.Context, corrID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetFileStatus")
	defer span.End()

	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		status, err = dbs.getFileStatus(ctx, corrID)
		count++
	}

	return status, err
}
func (dbs *SDAdb) getFileStatus(ctx context.Context, corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT event from sda.file_event_log WHERE correlation_id = $1 ORDER BY id DESC LIMIT 1;"

	var status string
	err := db.QueryRowContext(ctx, getFileID, corrID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetHeader retrieves the file header
func (dbs *SDAdb) GetHeader(ctx context.Context, fileID string) ([]byte, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetHeader")
	defer span.End()

	var (
		r     []byte
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		r, err = dbs.getHeader(ctx, fileID)
		count++
	}

	return r, err
}
func (dbs *SDAdb) getHeader(ctx context.Context, fileID string) ([]byte, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT header from sda.files WHERE id = $1;"

	var hexString string
	if err := db.QueryRowContext(ctx, query, fileID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// MarkCompleted marks the file as "COMPLETED"
func (dbs *SDAdb) SetVerified(ctx context.Context, file FileInfo, fileID string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.SetVerified")
	defer span.End()

	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.setVerified(ctx, file, fileID)
		count++
	}

	return err
}
func (dbs *SDAdb) setVerified(ctx context.Context, file FileInfo, fileID string) error {
	dbs.checkAndReconnectIfNeeded()

	const verified = "UPDATE sda.files SET decrypted_file_size = $1 WHERE id = $2;"
	if _, err := dbs.DB.ExecContext(ctx, verified, file.DecryptedSize, fileID); err != nil {
		return fmt.Errorf("setVerified error: %s", err.Error())
	}

	const addArchiveChecksum = `INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('ARCHIVED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;`

	if _, err := dbs.DB.ExecContext(ctx, addArchiveChecksum, fileID, file.ArchiveChecksum, "SHA256"); err != nil {
		return fmt.Errorf("addArchiveChecksum error: %s", err.Error())
	}

	const addUnencryptedChecksum = `INSERT INTO sda.checksums(file_id, checksum, type, source)
VALUES($1, $2, upper($3)::sda.checksum_algorithm, upper('UNENCRYPTED')::sda.checksum_source)
ON CONFLICT ON CONSTRAINT unique_checksum DO UPDATE SET checksum = EXCLUDED.checksum;`

	if _, err := dbs.DB.ExecContext(ctx, addUnencryptedChecksum, fileID, file.DecryptedChecksum, "SHA256"); err != nil {
		return fmt.Errorf("addUnencryptedChecksum error: %s", err.Error())
	}

	return nil
}

// GetArchived retrieves the location and size of archive
func (dbs *SDAdb) GetArchived(ctx context.Context, corrID string) (string, int, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetArchived")
	defer span.End()

	var (
		filePath string
		fileSize int
		err      error
		count    int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		filePath, fileSize, err = dbs.getArchived(ctx, corrID)
		count++
	}

	return filePath, fileSize, err
}
func (dbs *SDAdb) getArchived(ctx context.Context, corrID string) (string, int, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT archive_file_path, archive_file_size from sda.files WHERE id = $1;"

	var filePath string
	var fileSize int
	if err := db.QueryRowContext(ctx, query, corrID).Scan(&filePath, &fileSize); err != nil {
		return "", 0, err
	}

	return filePath, fileSize, nil
}

// CheckAccessionIdExists validates if an accessionID exists in the db
func (dbs *SDAdb) CheckAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.CheckAccessionIDExists")
	defer span.End()

	var err error
	var exists string
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		exists, err = dbs.checkAccessionIDExists(ctx, accessionID, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return exists, err
}
func (dbs *SDAdb) checkAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const sameID = "SELECT COUNT(id) FROM sda.files WHERE stable_id = $1 and id = $2;"
	var same int
	if err := db.QueryRowContext(ctx, sameID, accessionID, fileID).Scan(&same); err != nil {
		return "", err
	}

	if same > 0 {
		return "same", nil
	}

	const checkIDExist = "SELECT COUNT(id) FROM sda.files WHERE stable_id = $1;"
	var stableIDCount int
	if err := db.QueryRowContext(ctx, checkIDExist, accessionID).Scan(&stableIDCount); err != nil {
		return "", err
	}

	if stableIDCount > 0 {
		return "duplicate", nil
	}

	return "", nil
}

// SetAccessionID adds a stable id to a file
// identified by the user submitting it, inbox path and decrypted checksum
func (dbs *SDAdb) SetAccessionID(ctx context.Context, accessionID, fileID string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.SetAccessionID")
	defer span.End()

	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.setAccessionID(ctx, accessionID, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) setAccessionID(ctx context.Context, accessionID, fileID string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const setStableID = "UPDATE sda.files SET stable_id = $1 WHERE id = $2;"
	result, err := db.ExecContext(ctx, setStableID, accessionID, fileID)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// MapFilesToDataset maps a set of files to a dataset in the database
func (dbs *SDAdb) MapFilesToDataset(ctx context.Context, datasetID string, accessionIDs []string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.MapFilesToDataset")
	defer span.End()

	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.mapFilesToDataset(ctx, datasetID, accessionIDs)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) mapFilesToDataset(ctx context.Context, datasetID string, accessionIDs []string) error {
	dbs.checkAndReconnectIfNeeded()

	const getID = "SELECT id FROM sda.files WHERE stable_id = $1;"
	const dataset = "INSERT INTO sda.datasets (stable_id) VALUES ($1) ON CONFLICT DO NOTHING;"
	const mapping = "INSERT INTO sda.file_dataset (file_id, dataset_id) SELECT $1, id FROM sda.datasets WHERE stable_id = $2 ON CONFLICT DO NOTHING;"
	var fileID string

	db := dbs.DB
	_, err := db.ExecContext(ctx, dataset, datasetID)
	if err != nil {
		return err
	}

	transaction, _ := db.Begin()
	for _, accessionID := range accessionIDs {
		err := db.QueryRowContext(ctx, getID, accessionID).Scan(&fileID)
		if err != nil {
			log.Errorf("something went wrong with the DB query: %s", err.Error())
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err.Error())
			}

			return err
		}
		_, err = transaction.ExecContext(ctx, mapping, fileID, datasetID)
		if err != nil {
			log.Errorf("something went wrong with the DB transaction: %s", err.Error())
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err.Error())
			}

			return err
		}
	}

	return transaction.Commit()
}

// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
func (dbs *SDAdb) GetInboxPath(ctx context.Context, stableID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetInboxPath")
	defer span.End()

	var (
		err       error
		count     int
		inboxPath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		inboxPath, err = dbs.getInboxPath(ctx, stableID)
		count++
	}

	return inboxPath, err
}
func (dbs *SDAdb) getInboxPath(ctx context.Context, stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT submission_file_path from sda.files WHERE stable_id = $1;"

	var inboxPath string
	err := db.QueryRowContext(ctx, getFileID, stableID).Scan(&inboxPath)
	if err != nil {
		return "", err
	}

	return inboxPath, nil
}

// UpdateDatasetEvent marks the files in a dataset as "registered","released" or "deprecated"
func (dbs *SDAdb) UpdateDatasetEvent(ctx context.Context, datasetID, status, message string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.UpdateDatasetEvent")
	defer span.End()

	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.updateDatasetEvent(ctx, datasetID, status, message)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) updateDatasetEvent(ctx context.Context, datasetID, status, message string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const setStatus = "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES($1, $2, $3);"
	result, err := db.ExecContext(ctx, setStatus, datasetID, status, message)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// GetFileInfo returns info on a ingested file
func (dbs *SDAdb) GetFileInfo(ctx context.Context, id string) (FileInfo, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetFileInfo")
	defer span.End()

	var (
		err   error
		count int
		info  FileInfo
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		info, err = dbs.getFileInfo(ctx, id)
		count++
	}

	return info, err
}
func (dbs *SDAdb) getFileInfo(ctx context.Context, id string) (FileInfo, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT archive_file_path, archive_file_size from sda.files where id = $1;"
	const checkSum = `SELECT MAX(checksum) FILTER(where source = 'ARCHIVED') as Archived,
MAX(checksum) FILTER(where source = 'UNENCRYPTED') as Unencrypted,
MAX(checksum) FILTER(where source = 'UPLOADED') as Uploaded from sda.checksums where file_id = $1;`

	var info FileInfo
	if err := db.QueryRowContext(ctx, getFileID, id).Scan(&info.Path, &info.Size); err != nil {
		return FileInfo{}, err
	}

	var archivedChecksum, decryptedChecksum, uploadedChecksum sql.NullString
	if err := db.QueryRowContext(ctx, checkSum, id).Scan(&archivedChecksum, &decryptedChecksum, &uploadedChecksum); err != nil {
		return FileInfo{}, err
	}
	info.ArchiveChecksum = archivedChecksum.String
	info.DecryptedChecksum = decryptedChecksum.String
	info.UploadedChecksum = uploadedChecksum.String

	return info, nil
}

// GetHeaderForStableID retrieves the file header by using stable id
func (dbs *SDAdb) GetHeaderForStableID(ctx context.Context, stableID string) ([]byte, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetHeaderForStableID")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	const query = "SELECT header from sda.files WHERE stable_id = $1;"
	var hexString string
	if err := dbs.DB.QueryRowContext(ctx, query, stableID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// GetSyncData retrieves the file information needed to sync a dataset
func (dbs *SDAdb) GetSyncData(ctx context.Context, accessionID string) (SyncData, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetSyncData")
	defer span.End()

	var (
		s   SyncData
		err error
	)

	for count := 1; count <= RetryTimes; count++ {
		s, err = dbs.getSyncData(ctx, accessionID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return s, err
}

// getSyncData is the actual function performing work for GetSyncData
func (dbs *SDAdb) getSyncData(ctx context.Context, accessionID string) (SyncData, error) {
	dbs.checkAndReconnectIfNeeded()

	const query = "SELECT submission_user, submission_file_path from sda.files WHERE stable_id = $1;"
	var data SyncData
	if err := dbs.DB.QueryRowContext(ctx, query, accessionID).Scan(&data.User, &data.FilePath); err != nil {
		return SyncData{}, err
	}

	const checksum = "SELECT checksum from sda.checksums WHERE source = 'UNENCRYPTED' and file_id = (SELECT id FROM sda.files WHERE stable_id = $1);"
	if err := dbs.DB.QueryRowContext(ctx, checksum, accessionID).Scan(&data.Checksum); err != nil {
		return SyncData{}, err
	}

	return data, nil
}

// CheckIfDatasetExists checks if a dataset already is registered
func (dbs *SDAdb) CheckIfDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.CheckIfDatasetExists")
	defer span.End()

	var (
		ds  bool
		err error
	)

	for count := 1; count <= RetryTimes; count++ {
		ds, err = dbs.checkIfDatasetExists(ctx, datasetID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return ds, err
}

// getSyncData is the actual function performing work for GetSyncData
func (dbs *SDAdb) checkIfDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	dbs.checkAndReconnectIfNeeded()

	const query = "SELECT EXISTS(SELECT id from sda.datasets WHERE stable_id = $1);"
	var yesNo bool
	if err := dbs.DB.QueryRowContext(ctx, query, datasetID).Scan(&yesNo); err != nil {
		return yesNo, err
	}

	return yesNo, nil
}

// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
func (dbs *SDAdb) GetArchivePath(ctx context.Context, stableID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetArchivePath")
	defer span.End()

	var (
		err         error
		count       int
		archivePath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		archivePath, err = dbs.getArchivePath(ctx, stableID)
		count++
	}

	return archivePath, err
}
func (dbs *SDAdb) getArchivePath(ctx context.Context, stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT archive_file_path from sda.files WHERE stable_id = $1;"

	var archivePath string
	err := db.QueryRowContext(ctx, getFileID, stableID).Scan(&archivePath)
	if err != nil {
		return "", err
	}

	return archivePath, nil
}

// GetUserFiles retrieves all the files a user submitted
func (dbs *SDAdb) GetUserFiles(ctx context.Context, userID string, allData bool) ([]*SubmissionFileInfo, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetUserFiles")
	defer span.End()

	var err error

	files := []*SubmissionFileInfo{}

	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		files, err = dbs.getUserFiles(ctx, userID, allData)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return files, err
}

// getUserFiles is the actual function performing work for GetUserFiles
func (dbs *SDAdb) getUserFiles(ctx context.Context, userID string, allData bool) ([]*SubmissionFileInfo, error) {
	dbs.checkAndReconnectIfNeeded()

	files := []*SubmissionFileInfo{}
	db := dbs.DB

	// select all files (that are not part of a dataset) of the user, each one annotated with its latest event
	const query = `SELECT f.id, f.submission_file_path, f.stable_id, e.event, f.created_at FROM sda.files f
LEFT JOIN (SELECT DISTINCT ON (file_id) file_id, started_at, event FROM sda.file_event_log ORDER BY file_id, started_at DESC) e ON f.id = e.file_id WHERE f.submission_user = $1
AND NOT EXISTS (SELECT 1 FROM sda.file_dataset d WHERE f.id = d.file_id);`

	// nolint:rowserrcheck
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Iterate rows
	for rows.Next() {
		var accessionID sql.NullString
		// Read rows into struct
		fi := &SubmissionFileInfo{}
		err := rows.Scan(&fi.FileID, &fi.InboxPath, &accessionID, &fi.Status, &fi.CreateAt)
		if err != nil {
			return nil, err
		}

		if allData {
			fi.AccessionID = accessionID.String
		}

		// Add instance of struct (file) to array if the status is not disabled
		if fi.Status != "disabled" {
			files = append(files, fi)
		}
	}

	return files, nil
}

// get the correlation ID for a user-inbox_path combination
func (dbs *SDAdb) GetCorrID(ctx context.Context, user, path, accession string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetCorrID")
	defer span.End()

	var (
		corrID string
		err    error
	)
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		corrID, err = dbs.getCorrID(ctx, user, path, accession)
		if err == nil || strings.Contains(err.Error(), "sql: no rows in result set") {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return corrID, err
}
func (dbs *SDAdb) getCorrID(ctx context.Context, user, path, accession string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const query = `SELECT DISTINCT correlation_id FROM sda.file_event_log e
RIGHT JOIN sda.files f ON e.file_id = f.id
WHERE f.submission_file_path = $1 AND f.submission_user = $2 AND COALESCE(f.stable_id, '') = $3;`

	rows, err := db.QueryContext(ctx, query, path, user, accession)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var corrID sql.NullString
	for rows.Next() {
		err := rows.Scan(&corrID)
		if err != nil {
			return "", err
		}
		if corrID.Valid {
			return corrID.String, nil
		}
	}
	if rows.Err() != nil {
		return "", rows.Err()
	}

	return "", errors.New("sql: no rows in result set")
}

// list all users with files not yet assigned to a dataset
func (dbs *SDAdb) ListActiveUsers(ctx context.Context) ([]string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.ListActiveUsers")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	var users []string
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT submission_user FROM sda.files f WHERE NOT EXISTS (SELECT 1 FROM sda.file_dataset d WHERE f.id = d.file_id) ORDER BY submission_user ASC;")
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	defer rows.Close()

	for rows.Next() {
		var user string
		err := rows.Scan(&user)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	return users, nil
}

func (dbs *SDAdb) GetDatasetStatus(ctx context.Context, datasetID string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetDatasetStatus")
	defer span.End()

	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		status, err = dbs.getDatasetStatus(ctx, datasetID)
		count++
	}

	return status, err
}
func (dbs *SDAdb) getDatasetStatus(ctx context.Context, datasetID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getDatasetEvent = "SELECT event from sda.dataset_event_log WHERE dataset_id = $1 ORDER BY id DESC LIMIT 1;"

	var status string
	err := db.QueryRowContext(ctx, getDatasetEvent, datasetID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// AddKeyHash adds a key hash and key description in the encryption_keys table
func (dbs *SDAdb) AddKeyHash(ctx context.Context, keyHash, keyDescription string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.AddKeyHash")
	defer span.End()

	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.addKeyHash(ctx, keyHash, keyDescription)
		if err == nil || strings.Contains(err.Error(), "key hash already exists") {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}

func (dbs *SDAdb) addKeyHash(ctx context.Context, keyHash, keyDescription string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const query = "INSERT INTO sda.encryption_keys(key_hash, description) VALUES($1, $2) ON CONFLICT DO NOTHING;"

	result, err := db.ExecContext(ctx, query, keyHash, keyDescription)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("key hash already exists or no rows were updated")
	}

	return nil
}

func (dbs *SDAdb) SetKeyHash(ctx context.Context, keyHash, fileID string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.SetKeyHash")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	query := "UPDATE sda.files SET key_hash = $1 WHERE id = $2;"
	result, err := db.ExecContext(ctx, query, keyHash, fileID)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query, zero rows were changed")
	}
	log.Debugf("Successfully set key hash for file %v", fileID)

	return nil
}

type C4ghKeyHash struct {
	Hash         string `json:"hash"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
	DeprecatedAt string `json:"deprecated_at"`
}

// ListKeyHashes lists the hashes from the encryption_keys table
func (dbs *SDAdb) ListKeyHashes(ctx context.Context) ([]C4ghKeyHash, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.ListKeyHashes")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const query = "SELECT key_hash, description, created_at, deprecated_at FROM sda.encryption_keys ORDER BY created_at ASC;"

	hashList := []C4ghKeyHash{}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	defer rows.Close()

	for rows.Next() {
		h := &C4ghKeyHash{}
		depr := sql.NullString{}
		err := rows.Scan(&h.Hash, &h.Description, &h.CreatedAt, &depr)
		if err != nil {
			return nil, err
		}
		h.DeprecatedAt = depr.String

		hashList = append(hashList, *h)
	}

	return hashList, nil
}

func (dbs *SDAdb) DeprecateKeyHash(ctx context.Context, keyHash string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.DeprecateKeyHash")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const query = "UPDATE sda.encryption_keys set deprecated_at = NOW() WHERE key_hash = $1 AND deprecated_at IS NULL;"
	result, err := db.ExecContext(ctx, query, keyHash)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("key hash not found or already deprecated")
	}

	return nil
}

// ListDatasets lists all datasets as well as the status
func (dbs *SDAdb) ListDatasets(ctx context.Context) ([]*DatasetInfo, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.ListDatasets")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	var datasets []*DatasetInfo
	rows, err := db.QueryContext(ctx, "SELECT dataset_id,event,event_date FROM sda.dataset_event_log WHERE (dataset_id, event_date) IN (SELECT dataset_id,max(event_date) FROM sda.dataset_event_log GROUP BY dataset_id);")
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	for rows.Next() {
		var di DatasetInfo
		err := rows.Scan(&di.DatasetID, &di.Status, &di.Timestamp)
		if err != nil {
			return nil, err
		}

		datasets = append(datasets, &di)
	}
	rows.Close()

	return datasets, nil
}

func (dbs *SDAdb) ListUserDatasets(ctx context.Context, submissionUser string) ([]DatasetInfo, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.ListUserDatasets")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	query := `SELECT dataset_id,event,event_date FROM sda.dataset_event_log WHERE
(dataset_id, event_date) IN (
	SELECT dataset_id,max(event_date) FROM sda.dataset_event_log WHERE 
	dataset_id IN (
		SELECT stable_id FROM sda.datasets WHERE 
		id IN (
			SELECT DISTINCT dataset_id FROM sda.file_dataset WHERE 
			file_id IN (
				SELECT id FROM sda.files WHERE submission_user = $1 AND stable_id IS NOT NULL
			)
		)
	)
	GROUP BY dataset_id
);`

	rows, err := db.QueryContext(ctx, query, submissionUser)
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	var datasets []DatasetInfo
	for rows.Next() {
		var di DatasetInfo
		err := rows.Scan(&di.DatasetID, &di.Status, &di.Timestamp)
		if err != nil {
			return nil, err
		}

		datasets = append(datasets, di)
	}
	rows.Close()

	return datasets, nil
}

func (dbs *SDAdb) UpdateUserInfo(ctx context.Context, userID, name, email string, groups []string) error {
	ctx, span := observability.GetTracer().Start(ctx, "storage.UpdateUserInfo")
	defer span.End()

	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.updateUserInfo(ctx, userID, name, email, groups)
		count++
	}

	return err
}
func (dbs *SDAdb) updateUserInfo(ctx context.Context, userID, name, email string, groups []string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = `INSERT INTO sda.userinfo(id, name, email, groups) VALUES($1, $2, $3, $4)
ON CONFLICT (id)
DO UPDATE SET name = excluded.name, email = excluded.email, groups = excluded.groups;`

	result, err := db.ExecContext(ctx, query, userID, name, email, pq.Array(groups))
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

func (dbs *SDAdb) GetReVerificationData(ctx context.Context, accessionID string) (schema.IngestionVerification, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetReVerificationData")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	reVerify := schema.IngestionVerification{ReVerify: true}

	const query = "SELECT archive_file_path,id,submission_file_path,submission_user FROM sda.files where stable_id = $1;"
	err := db.QueryRowContext(ctx, query, accessionID).Scan(&reVerify.ArchivePath, &reVerify.FileID, &reVerify.FilePath, &reVerify.User)
	if err != nil {
		return schema.IngestionVerification{}, err
	}

	var checksum schema.Checksums
	const archiveChecksum = "SELECT type,checksum from sda.checksums WHERE file_id = $1 AND source = 'ARCHIVED';"
	if err := db.QueryRowContext(ctx, archiveChecksum, reVerify.FileID).Scan(&checksum.Type, &checksum.Value); err != nil {
		log.Errorln(err.Error())

		return schema.IngestionVerification{}, err
	}
	checksum.Type = strings.ToLower(checksum.Type)
	reVerify.EncryptedChecksums = append(reVerify.EncryptedChecksums, checksum)

	return reVerify, nil
}

func (dbs *SDAdb) GetDecryptedChecksum(ctx context.Context, id string) (string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetDecryptedChecksum")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	var unencryptedChecksum string
	if err := db.QueryRowContext(ctx, "SELECT checksum from sda.checksums WHERE file_id = $1 AND source = 'UNENCRYPTED';", id).Scan(&unencryptedChecksum); err != nil {
		return "", err
	}

	return unencryptedChecksum, nil
}

func (dbs *SDAdb) GetDatasetFiles(ctx context.Context, dataset string) ([]string, error) {
	ctx, span := observability.GetTracer().Start(ctx, "storage.GetDatasetFiles")
	defer span.End()

	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	var accessions []string
	rows, err := db.QueryContext(ctx, "SELECT stable_id FROM sda.files WHERE id IN (SELECT file_id FROM sda.file_dataset WHERE dataset_id = (SELECT id FROM sda.datasets WHERE stable_id = $1));", dataset)
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	defer rows.Close()

	for rows.Next() {
		var accession string
		err := rows.Scan(&accession)
		if err != nil {
			return nil, err
		}

		accessions = append(accessions, accession)
	}

	return accessions, nil
}
