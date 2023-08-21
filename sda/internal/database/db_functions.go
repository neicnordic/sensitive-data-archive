// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	log "github.com/sirupsen/logrus"
)

// RegisterFile inserts a file in the database, along with a "registered" log
// event. If the file already exists in the database, the entry is updated, but
// a new file event is always inserted.
func (dbs *SDAdb) RegisterFile(uploadPath, uploadUser string) (string, error) {

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return "", errors.New("database schema v4 required for RegisterFile()")
	}

	query := "SELECT sda.register_file($1, $2)"

	var fileID string

	err := dbs.DB.QueryRow(query, uploadPath, uploadUser).Scan(&fileID)

	return fileID, err
}

// UpdateFileEventLog updates the status in of the file in the database.
// The message parameter is the rabbitmq message sent on file upload.
func (dbs *SDAdb) UpdateFileEventLog(fileID, event, userID, message string) error {

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return errors.New("database schema v4 required for UpdateFileEventLog()")
	}

	query := "INSERT INTO sda.file_event_log(file_id, event, user_id, message) VALUES ($1, $2, $3, $4)"
	_, err := dbs.DB.Exec(query, fileID, event, userID, message)

	return err
}

func (dbs *SDAdb) GetFileID(corrID string) (string, error) {
	var (
		err   error
		count int
		ID    string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		ID, err = dbs.getFileID(corrID)
		count++
	}

	return ID, err
}
func (dbs *SDAdb) getFileID(corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT DISTINCT file_id FROM sda.file_event_log where correlation_id = $1;"

	var fileID string
	err := db.QueryRow(getFileID, corrID).Scan(&fileID)
	if err != nil {
		return "", err
	}

	return fileID, nil
}

func (dbs *SDAdb) UpdateFileStatus(fileUUID, event, corrID, user, message string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.updateFileStatus(fileUUID, event, corrID, user, message)
		count++
	}

	return err
}
func (dbs *SDAdb) updateFileStatus(fileUUID, event, corrID, user, message string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, message) VALUES($1, $2, $3, $4, $5);"

	result, err := db.Exec(query, fileUUID, event, corrID, user, message)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// StoreHeader stores the file header in the database
func (dbs *SDAdb) StoreHeader(header []byte, id string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.storeHeader(header, id)
		count++
	}

	return err
}
func (dbs *SDAdb) storeHeader(header []byte, id string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "UPDATE sda.files SET header = $1 WHERE id = $2;"
	result, err := db.Exec(query, hex.EncodeToString(header), id)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// SetArchived marks the file as 'ARCHIVED'
func (dbs *SDAdb) SetArchived(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.setArchived(file, fileID, corrID)
		count++
	}

	return err
}
func (dbs *SDAdb) setArchived(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT sda.set_archived($1, $2, $3, $4, $5, $6);"
	_, err := db.Exec(query,
		fileID,
		corrID,
		file.Path,
		file.Size,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
	)

	return err
}

func (dbs *SDAdb) GetFileStatus(corrID string) (string, error) {
	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		status, err = dbs.getFileStatus(corrID)
		count++
	}

	return status, err
}
func (dbs *SDAdb) getFileStatus(corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT event from sda.file_event_log WHERE correlation_id = $1 ORDER BY id DESC LIMIT 1;"

	var status string
	err := db.QueryRow(getFileID, corrID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetHeader retrieves the file header
func (dbs *SDAdb) GetHeader(fileID string) ([]byte, error) {
	var (
		r     []byte
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		r, err = dbs.getHeader(fileID)
		count++
	}

	return r, err
}
func (dbs *SDAdb) getHeader(fileID string) ([]byte, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT header from sda.files WHERE id = $1"

	var hexString string
	if err := db.QueryRow(query, fileID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// MarkCompleted marks the file as "COMPLETED"
func (dbs *SDAdb) MarkCompleted(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.markCompleted(file, fileID, corrID)
		count++
	}

	return err
}
func (dbs *SDAdb) markCompleted(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const completed = "SELECT sda.set_verified($1, $2, $3, $4, $5, $6, $7);"
	_, err := db.Exec(completed,
		fileID,
		corrID,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
		file.DecryptedSize,
		fmt.Sprintf("%x", file.DecryptedChecksum.Sum(nil)),
		hashType(file.DecryptedChecksum),
	)

	return err
}

// GetArchived retrieves the location and size of archive
func (dbs *SDAdb) GetArchived(user, filepath, checksum string) (string, int, error) {
	var (
		filePath string
		fileSize int
		err      error
		count    int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		filePath, fileSize, err = dbs.getArchived(user, filepath, checksum)
		count++
	}

	return filePath, fileSize, err
}
func (dbs *SDAdb) getArchived(user, filepath, checksum string) (string, int, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT archive_path, archive_filesize from local_ega.files WHERE " +
		"elixir_id = $1 and inbox_path = $2 and decrypted_file_checksum = $3 and status in ('COMPLETED', 'READY');"

	var filePath string
	var fileSize int
	if err := db.QueryRow(query, user, filepath, checksum).Scan(&filePath, &fileSize); err != nil {
		return "", 0, err
	}

	return filePath, fileSize, nil
}

// CheckAccessionIdExists validates if an accessionID exists in the db
func (dbs *SDAdb) CheckAccessionIDExists(accessionID string) (bool, error) {
	var err error
	var exists bool
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		exists, err = dbs.checkAccessionIDExists(accessionID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return exists, err
}
func (dbs *SDAdb) checkAccessionIDExists(accessionID string) (bool, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const checkIDExist = "SELECT COUNT(*) FROM sda.files WHERE stable_id = $1;"
	var stableIDCount int
	if err := db.QueryRow(checkIDExist, accessionID).Scan(&stableIDCount); err != nil {
		return false, err
	}

	if stableIDCount >= 1 {
		return true, nil
	}

	return false, nil
}

// SetAccessionID adds a stable id to a file
// identified by the user submitting it, inbox path and decrypted checksum
func (dbs *SDAdb) SetAccessionID(accessionID, fileID string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.setAccessionID(accessionID, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) setAccessionID(accessionID, fileID string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const setStableID = "UPDATE sda.files SET stable_id = $1 WHERE id = $2;"
	result, err := db.Exec(setStableID, accessionID, fileID)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// MapFilesToDataset maps a set of files to a dataset in the database
func (dbs *SDAdb) MapFilesToDataset(datasetID string, accessionIDs []string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.mapFilesToDataset(datasetID, accessionIDs)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) mapFilesToDataset(datasetID string, accessionIDs []string) error {
	dbs.checkAndReconnectIfNeeded()

	const getID = "SELECT id FROM sda.files WHERE stable_id = $1;"
	const dataset = "INSERT INTO sda.datasets (stable_id) VALUES ($1) ON CONFLICT DO NOTHING;"
	const mapping = "INSERT INTO sda.file_dataset (file_id, dataset_id) SELECT $1, id FROM sda.datasets WHERE stable_id = $2 ON CONFLICT DO NOTHING;"
	var fileID string

	db := dbs.DB
	_, err := db.Exec(dataset, datasetID)
	if err != nil {
		return err
	}

	transaction, _ := db.Begin()
	for _, accessionID := range accessionIDs {
		err := db.QueryRow(getID, accessionID).Scan(&fileID)
		if err != nil {
			log.Errorf("something went wrong with the DB query: %s", err)
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err)
			}

			return err
		}
		_, err = transaction.Exec(mapping, fileID, datasetID)
		if err != nil {
			log.Errorf("something went wrong with the DB transaction: %s", err)
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err)
			}

			return err
		}
	}

	return transaction.Commit()
}

// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
func (dbs *SDAdb) GetInboxPath(stableID string) (string, error) {
	var (
		err       error
		count     int
		inboxPath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		inboxPath, err = dbs.getInboxPath(stableID)
		count++
	}

	return inboxPath, err
}
func (dbs *SDAdb) getInboxPath(stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT submission_file_path from sda.files WHERE stable_id = $1;"

	var inboxPath string
	err := db.QueryRow(getFileID, stableID).Scan(&inboxPath)
	if err != nil {
		return "", err
	}

	return inboxPath, nil
}

// UpdateDatasetEvent marks the files in a dataset as "ready" or "disabled"
func (dbs *SDAdb) UpdateDatasetEvent(datasetID, status, message string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.updateDatasetEvent(datasetID, status, message)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) updateDatasetEvent(datasetID, status, message string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const setStatus = "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES($1, $2, $3);"
	result, err := db.Exec(setStatus, datasetID, status, message)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil

}
