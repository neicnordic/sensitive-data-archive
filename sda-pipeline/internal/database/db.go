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
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	// Needed implicitly to enable Postgres driver
	_ "github.com/lib/pq"
)

// Database defines methods to be implemented by SQLdb
type Database interface {
	GetHeader(fileID int) ([]byte, error)
	MarkCompleted(checksum string, fileID int) error
	MarkReady(accessionID, user, filepath, checksum string) error
	GetArchived(user, filepath, checksum string) (string, int, error)
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
func (dbs *SQLdb) GetHeader(fileID int) ([]byte, error) {
	var (
		r     []byte = nil
		err   error  = nil
		count int    = 0
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		r, err = dbs.getHeader(fileID)
		count++
	}
	return r, err
}

// getHeader is the actual function performing work for GetHeader
func (dbs *SQLdb) getHeader(fileID int) ([]byte, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT header from local_ega.files WHERE id = $1"

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

// GetHeaderForStableId retrieves the file header by using stable id
func (dbs *SQLdb) GetHeaderForStableId(stableID string) (string, error) {
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
func (dbs *SQLdb) MarkCompleted(file FileInfo, fileID int) error {
	var (
		err   error = nil
		count int   = 0
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.markCompleted(file, fileID)
		count++
	}

	return err
}

// markCompleted performs actual work for MarkCompleted
func (dbs *SQLdb) markCompleted(file FileInfo, fileID int) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const completed = "UPDATE local_ega.files SET status = 'COMPLETED', " +
		"archive_filesize = $2, " +
		"archive_file_checksum = $3, " +
		"archive_file_checksum_type = $4, " +
		"decrypted_file_size = $5, " +
		"decrypted_file_checksum = $6, " +
		"decrypted_file_checksum_type = $7 " +
		"WHERE id = $1;"
	result, err := db.Exec(completed,
		fileID,
		file.Size,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
		file.DecryptedSize,
		fmt.Sprintf("%x", file.DecryptedChecksum.Sum(nil)),
		hashType(file.DecryptedChecksum))
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// InsertFile inserts a file in the database
func (dbs *SQLdb) InsertFile(filename, user string) (int64, error) {
	var (
		err   error = nil
		r     int64 = 0
		count int   = 0
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		r, err = dbs.insertFile(filename, user)
		count++
	}
	return r, err
}

// insertFile performs actual work for InsertFile
func (dbs *SQLdb) insertFile(filename, user string) (int64, error) {
	dbs.checkAndReconnectIfNeeded()

	// Not really idempotent, but close enough for us

	db := dbs.DB
	const query = "INSERT INTO local_ega.main(submission_file_path, " +
		"submission_file_extension, " +
		"submission_user, " +
		"status, " +
		"encryption_method) " +
		"VALUES($1, $2, $3,'INIT', 'CRYPT4GH') RETURNING id;"
	var fileID int64
	err := db.QueryRow(query, filename, strings.Replace(filepath.Ext(filename), ".", "", -1), user).Scan(&fileID)
	if err != nil {
		return 0, err
	}
	return fileID, nil
}

// StoreHeader stores the file header in the database
func (dbs *SQLdb) StoreHeader(header []byte, id int64) error {
	var (
		err   error = nil
		count int   = 0
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.storeHeader(header, id)
		count++
	}
	return err
}

// storeHeader performs actual work for StoreHeader
func (dbs *SQLdb) storeHeader(header []byte, id int64) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "UPDATE local_ega.files SET header = $1 WHERE id = $2;"
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
func (dbs *SQLdb) SetArchived(file FileInfo, id int64) error {
	var (
		err   error = nil
		count int   = 0
	)

	for count == 0 || (err != nil && count < dbRetryTimes) {
		err = dbs.setArchived(file, id)
		count++
	}
	return err
}

// setArchived performs actual work for SetArchived
func (dbs *SQLdb) setArchived(file FileInfo, id int64) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "UPDATE local_ega.files SET status = 'ARCHIVED', " +
		"archive_path = $1, " +
		"archive_filesize = $2, " +
		"inbox_file_checksum = $3, " +
		"inbox_file_checksum_type = $4 " +
		"WHERE id = $5;"
	result, err := db.Exec(query,
		file.Path,
		file.Size,
		fmt.Sprintf("%x", file.Checksum.Sum(nil)),
		hashType(file.Checksum),
		id)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}
	return nil
}

// MarkReady marks the file as "READY"
func (dbs *SQLdb) MarkReady(accessionID, user, filepath, checksum string) error {

	var err error

	// 3, 9, 27, 81, 243 seconds between each retry event.
	for count := 1; count <= dbRetryTimes; count++ {
		err = dbs.markReady(accessionID, user, filepath, checksum)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return err
}

// MarkReady marks the file as "READY"
func (dbs *SQLdb) markReady(accessionID, user, filepath, checksum string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const ready = "UPDATE local_ega.files SET status = 'READY', stable_id = $1 WHERE " +
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

	const getID = "SELECT file_id FROM local_ega.archive_files WHERE stable_id = $1"
	const mapping = "INSERT INTO local_ega_ebi.filedataset (file_id, dataset_stable_id) " +
		"VALUES ($1, $2) ON CONFLICT " +
		"DO NOTHING;"
	db := dbs.DB
	var fileID int64
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
		filePath string = ""
		fileSize int    = 0
		err      error  = nil
		count    int    = 0
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

// Close terminates the connection to the database
func (dbs *SQLdb) Close() {
	db := dbs.DB
	db.Close()
}
