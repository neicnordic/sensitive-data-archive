// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"time"

	log "github.com/sirupsen/logrus"

	// Needed implicitly to enable Postgres driver
	_ "github.com/lib/pq"
)

// Database defines methods to be implemented by SQLdb
type Database interface {
	GetArchived(user, filepath, checksum string) (string, int, error)
	GetFileID(corrID string) (string, error)
	GetHeader(fileID string) ([]byte, error)
	MarkCompleted(checksum string, fileID int) error
	MarkReady(accessionID, user, filepath, checksum string) error
	RegisterFile(filePath, user string) (string, error)
	SetArchived(file FileInfo, id, corrID string) error
	UpdateFileStatus(fileUUID, event, corrID, user, message string) error
	Close()
}

// SQLdb struct that acts as a receiver for the DB update methods
type SQLdb struct {
	DB       *sql.DB
	ConnInfo string
}

// DBConf stores information about the database backend
type DBConf struct {
	Host       string
	Port       int
	User       string
	Password   string
	Database   string
	CACert     string
	SslMode    string
	ClientCert string
	ClientKey  string
}

// FileInfo is used by ingest for file metadata (path, size, checksum)
type FileInfo struct {
	Checksum          hash.Hash
	Size              int64
	Path              string
	DecryptedChecksum hash.Hash
	DecryptedSize     int64
}

// dbRetryTimes is the number of times to retry the same function if it fails
var dbRetryTimes = 5

// dbReconnectTimeout is how long to try to re-establish a connection to the database
var dbReconnectTimeout = 5 * time.Minute

// dbReconnectSleep is how long to wait between attempts to connect to the database
var dbReconnectSleep = 5 * time.Second

// sqlOpen is an internal variable to ease testing
var sqlOpen = sql.Open

// logFatalf is an internal variable to ease testing
var logFatalf = log.Fatalf

// hashType returns the identification string for the hash type
func hashType(h hash.Hash) string {
	// TODO: Support/check type
	return "SHA256"
}

// NewDB creates a new DB connection
func NewDB(config DBConf) (*SQLdb, error) {
	connInfo := buildConnInfo(config)

	log.Debugf("Connecting to DB %s:%d on database: %s with user: %s", config.Host, config.Port, config.Database, config.User)
	db, err := sqlOpen("postgres", connInfo)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	return &SQLdb{DB: db, ConnInfo: connInfo}, nil
}

// buildConnInfo builds a connection string for the database
func buildConnInfo(config DBConf) string {
	connInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.Database, config.SslMode)

	if config.SslMode == "disable" {
		return connInfo
	}

	if config.CACert != "" {
		connInfo += fmt.Sprintf(" sslrootcert=%s", config.CACert)
	}

	if config.ClientCert != "" {
		connInfo += fmt.Sprintf(" sslcert=%s", config.ClientCert)
	}

	if config.ClientKey != "" {
		connInfo += fmt.Sprintf(" sslkey=%s", config.ClientKey)
	}

	return connInfo
}

func (dbs *SQLdb) Reconnect() {
	dbs.DB.Close()
	dbs.DB, _ = sqlOpen("postgres", dbs.ConnInfo)
}

// checkAndReconnectIfNeeded validates the current connection with a ping
// and tries to reconnect if necessary
func (dbs *SQLdb) checkAndReconnectIfNeeded() {
	start := time.Now()

	for dbs.DB.Ping() != nil {
		log.Errorln("Database unreachable, reconnecting")
		dbs.DB.Close()

		if time.Since(start) > dbReconnectTimeout {
			logFatalf("Could not reconnect to failed database in reasonable time, giving up")
		}
		time.Sleep(dbReconnectSleep)
		log.Debugln("Reconnecting to DB")
		dbs.DB, _ = sqlOpen("postgres", dbs.ConnInfo)
	}

}

// GetHeader retrieves the file header
func (dbs *SQLdb) GetHeader(fileID string) ([]byte, error) {
	var (
		r     []byte
		err   error
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		r, err = dbs.getHeader(fileID)
		count++
	}

	return r, err
}

// getHeader is the actual function performing work for GetHeader
func (dbs *SQLdb) getHeader(fileID string) ([]byte, error) {
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

// GetHeaderForStableID retrieves the file header by using stable id
func (dbs *SQLdb) GetHeaderForStableID(stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT header from local_ega.files WHERE stable_id = $1"

	var header string
	if err := db.QueryRow(query, stableID).Scan(&header); err != nil {
		return "", err
	}

	return header, nil
}

// MarkCompleted marks the file as "COMPLETED"
func (dbs *SQLdb) MarkCompleted(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.markCompleted(file, fileID, corrID)
		count++
	}

	return err
}

// markCompleted performs actual work for MarkCompleted
func (dbs *SQLdb) markCompleted(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const completed = "SELECT sda.set_verified($1, $2, $3, $4, $5, $6, $7);"
	result, err := db.Exec(completed,
		fileID,
		corrID,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
		file.DecryptedSize,
		fmt.Sprintf("%x", file.DecryptedChecksum.Sum(nil)),
		hashType(file.DecryptedChecksum),
	)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// RegisterFile inserts a file in the database
func (dbs *SQLdb) RegisterFile(filePath, user string) (string, error) {
	var (
		err   error
		id    string
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		id, err = dbs.registerFile(filePath, user)
		count++
	}

	return id, err
}
func (dbs *SQLdb) registerFile(filePath, user string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const query = "SELECT sda.register_file($1, $2);"
	var fileUUID string
	err := db.QueryRow(query, filePath, user).Scan(&fileUUID)
	if err != nil {
		return "", err
	}

	return fileUUID, nil
}

func (dbs *SQLdb) GetFileID(corrID string) (string, error) {
	var (
		err   error
		count int
		ID    string
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		ID, err = dbs.getFileID(corrID)
		count++
	}

	return ID, err
}
func (dbs *SQLdb) getFileID(corrID string) (string, error) {
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

// StoreHeader stores the file header in the database
func (dbs *SQLdb) StoreHeader(header []byte, id string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.storeHeader(header, id)
		count++
	}

	return err
}

// storeHeader performs actual work for StoreHeader
func (dbs *SQLdb) storeHeader(header []byte, id string) error {
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
func (dbs *SQLdb) SetArchived(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.setArchived(file, fileID, corrID)
		count++
	}

	return err
}

// setArchived performs actual work for SetArchived
func (dbs *SQLdb) setArchived(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT sda.set_archived($1, $2, $3, $4, $5, $6);"
	result, err := db.Exec(query,
		fileID,
		corrID,
		file.Path,
		file.Size,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
	)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// CheckAccessionIdExists validates if an accessionID exists in the db
func (dbs *SQLdb) CheckAccessionIDExists(accessionID string) (bool, error) {

	var err error
	var exists bool

	// 3, 9, 27, 81, 243 seconds between each retry event.
	for count := 1; count <= dbRetryTimes; count++ {
		exists, err = dbs.checkAccessionIDExists(accessionID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return exists, err
}

// checkAccessionIdExists validates if an accessionID exists in the db
func (dbs *SQLdb) checkAccessionIDExists(accessionID string) (bool, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB

	const checkIDExist = "SELECT COUNT(*) FROM sda.files WHERE stable_id = $1;"

	var stableIDCount int
	if err := db.QueryRow(checkIDExist, accessionID).Scan(&stableIDCount); err != nil {
		return false, err
	}
	log.Debugf("existing accession id count is: %d", stableIDCount)
	if stableIDCount >= 1 {
		return true, nil
	}

	return false, nil
}

// UpdateDatasetEvent marks the files in a dataset as "ready" or "disabled"
func (dbs *SQLdb) UpdateDatasetEvent(datasetID, status, correlationID, user string) error {

	var err error

	// 3, 9, 27, 81, 243 seconds between each retry event.
	for count := 1; count <= dbRetryTimes; count++ {
		err = dbs.updateDatasetEvent(datasetID, status, correlationID, user)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return err
}

// updateDatasetEvent marks the files in a dataset as "ready" or "disabled"
func (dbs *SQLdb) updateDatasetEvent(datasetID, status, correlationID, user string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const dataset = "SELECT id FROM sda.datasets WHERE stable_id = $1;"
	const markFile = "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id) " +
		"SELECT file_id, $2, $3, $4 from sda.file_dataset " +
		"WHERE dataset_id = $1;"

	var datasetInternalID int
	if err := db.QueryRow(dataset, datasetID).Scan(&datasetInternalID); err != nil {
		return err
	}

	result, err := db.Exec(markFile, datasetInternalID, status, correlationID, user)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil

}

// SetAccessionID adds a stable id to a file
// identified by the user submitting it, inbox path and decrypted checksum
func (dbs *SQLdb) SetAccessionID(accessionID, user, filepath, checksum string) error {

	var err error

	// 3, 9, 27, 81, 243 seconds between each retry event.
	for count := 1; count <= dbRetryTimes; count++ {
		err = dbs.setAccessionID(accessionID, user, filepath, checksum)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return err
}

// setAccessionID (actual operation) adds a stable id to a file
// identified by the user submitting it, inbox path and decrypted checksum
func (dbs *SQLdb) setAccessionID(accessionID, user, filepath, checksum string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB

	const ready = "UPDATE local_ega.files SET stable_id = $1 WHERE " +
		"elixir_id = $2 and inbox_path = $3 and decrypted_file_checksum = $4 and status = 'COMPLETED';"
	result, err := db.Exec(ready, accessionID, user, filepath, checksum)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// MapFilesToDataset maps a set of files to a dataset in the database
func (dbs *SQLdb) MapFilesToDataset(datasetID string, accessionIDs []string) error {

	var err error

	// 3, 9, 27, 81, 243 seconds between each retry event.
	for count := 1; count <= dbRetryTimes; count++ {
		err = dbs.mapFilesToDataset(datasetID, accessionIDs)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return err
}

// mapFilesToDataset performs the real work of MapFilesToDataset
func (dbs *SQLdb) mapFilesToDataset(datasetID string, accessionIDs []string) error {
	dbs.checkAndReconnectIfNeeded()

	const getID = "SELECT id FROM sda.files WHERE stable_id = $1;"
	const dataset = "INSERT INTO sda.datasets (stable_id) VALUES ($1) " +
		"ON CONFLICT DO NOTHING;"
	const mapping = "INSERT INTO sda.file_dataset (file_id, dataset_id) " +
		"SELECT $1, id FROM sda.datasets WHERE stable_id = $2 ON CONFLICT " +
		"DO NOTHING;"
	db := dbs.DB
	var fileID string
	_, err := db.Exec(dataset, datasetID)
	if err != nil {
		return err
	}
	transaction, _ := db.Begin()
	for _, accessionID := range accessionIDs {
		err := db.QueryRow(getID, accessionID).Scan(&fileID)
		if err != nil {
			log.Errorf("something went wrong with the DB query: %s", err)
			if e := transaction.Rollback(); e != nil {
				log.Errorf("failed to rollback the transaction: %s", e)
			}

			return err
		}
		_, err = transaction.Exec(mapping, fileID, datasetID)
		if err != nil {
			log.Errorf("something went wrong with the DB transaction: %s", err)
			if e := transaction.Rollback(); e != nil {
				log.Errorf("failed to rollback the transaction: %s", e)
			}

			return err
		}
	}

	return transaction.Commit()
}

// GetArchived retrieves the location and size of archive
func (dbs *SQLdb) GetArchived(user, filepath, checksum string) (string, int, error) {
	var (
		filePath string
		fileSize int
		err      error
		count    int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		filePath, fileSize, err = dbs.getArchived(user, filepath, checksum)
		count++
	}

	return filePath, fileSize, err
}

// getArchived is the actual function performing work for GetArchived
func (dbs *SQLdb) getArchived(user, filepath, checksum string) (string, int, error) {
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

func (dbs *SQLdb) GetVersion() (int, error) {
	dbs.checkAndReconnectIfNeeded()
	log.Debug("Fetching database schema version")

	query := "SELECT MAX(version) FROM sda.dbschema_version"
	var dbVersion = -1
	err := dbs.DB.QueryRow(query).Scan(&dbVersion)

	return dbVersion, err
}

func (dbs *SQLdb) UpdateFileStatus(fileUUID, event, corrID, user, message string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.updateFileStatus(fileUUID, event, corrID, user, message)
		count++
	}

	return err
}
func (dbs *SQLdb) updateFileStatus(fileUUID, event, corrID, user, message string) error {
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

func (dbs *SQLdb) GetFileStatus(corrID string) (string, error) {
	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		status, err = dbs.getFileStatus(corrID)
		count++
	}

	return status, err
}
func (dbs *SQLdb) getFileStatus(corrID string) (string, error) {
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

func (dbs *SQLdb) GetInboxPath(stableID string) (string, error) {
	var (
		err       error
		count     int
		inboxPath string
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		inboxPath, err = dbs.getInboxPath(stableID)
		count++
	}

	return inboxPath, err
}
func (dbs *SQLdb) getInboxPath(stableID string) (string, error) {
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

// Close terminates the connection to the database
func (dbs *SQLdb) Close() {
	db := dbs.DB
	db.Close()
}
