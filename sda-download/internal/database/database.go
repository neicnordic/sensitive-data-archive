package database

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"

	// enables postgres driver
	_ "github.com/lib/pq"
)

// DB is exported for other packages
var DB *SQLdb

// SQLdb struct that acts as a receiver for the DB update methods
type SQLdb struct {
	DB       *sql.DB
	ConnInfo string
}

// FileInfo  is returned by the metadata/datasets/*dataset/files endpoint
type FileInfo struct {
	FileID                    string `json:"fileId"`
	DisplayFileName           string `json:"displayFileName"`
	FilePath                  string `json:"filePath"`
	DecryptedFileSize         int64  `json:"decryptedFileSize"`
	DecryptedFileChecksum     string `json:"decryptedFileChecksum"`
	DecryptedFileChecksumType string `json:"decryptedFileChecksumType"`
}

type DatasetInfo struct {
	DatasetID string `json:"datasetId"`
	CreatedAt string `json:"createdAt"`
}

// dbRetryTimes is the number of times to retry the same function if it fails
var dbRetryTimes = 3

// dbReconnectTimeout is how long to try to re-establish a connection to the database
var dbReconnectTimeout = 5 * time.Minute

// dbReconnectSleep is how long to wait between attempts to connect to the database
var dbReconnectSleep = 1 * time.Second

// sqlOpen is an internal variable to ease testing
var sqlOpen = sql.Open

// logFatalf is an internal variable to ease testing
var logFatalf = log.Fatalf

func sanitizeString(str string) string {
	var pattern = regexp.MustCompile(`([A-Za-z0-9-_:.]+)`)

	return pattern.ReplaceAllString(str, "[identifier]: $1")
}

// NewDB creates a new DB connection
func NewDB(conf config.DatabaseConfig) (*SQLdb, error) {
	connInfo := buildConnInfo(conf)

	log.Debugf("Connecting to DB %s:%d on database: %s with user: %s", conf.Host, conf.Port, conf.Database, conf.User)
	db, err := sqlOpen("postgres", connInfo)
	if err != nil {
		log.Errorf("failed to connect to database, %s", err)

		return nil, err
	}

	if err = db.Ping(); err != nil {
		log.Errorf("could not get response from database, %s", err)

		return nil, err
	}

	log.Debug("database connection formed")

	return &SQLdb{DB: db, ConnInfo: connInfo}, nil
}

// buildConnInfo builds a connection string for the database
func buildConnInfo(conf config.DatabaseConfig) string {
	connInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		conf.Host, conf.Port, conf.User, conf.Password, conf.Database, conf.SslMode)

	if conf.SslMode == "disable" {
		return connInfo
	}

	if conf.CACert != "" {
		connInfo += fmt.Sprintf(" sslrootcert=%s", conf.CACert)
	}

	if conf.ClientCert != "" {
		connInfo += fmt.Sprintf(" sslcert=%s", conf.ClientCert)
	}

	if conf.ClientKey != "" {
		connInfo += fmt.Sprintf(" sslkey=%s", conf.ClientKey)
	}

	return connInfo
}

// checkAndReconnectIfNeeded validates the current connection with a ping
// and tries to reconnect if necessary
func (dbs *SQLdb) checkAndReconnectIfNeeded() {
	start := time.Now()

	for dbs.DB.Ping() != nil {
		log.Errorln("Database unreachable, reconnecting")
		_ = dbs.DB.Close()

		if time.Since(start) > dbReconnectTimeout {
			logFatalf("Could not reconnect to failed database in reasonable time, giving up")
		}
		time.Sleep(dbReconnectSleep)
		log.Debugln("Reconnecting to DB")
		dbs.DB, _ = sqlOpen("postgres", dbs.ConnInfo)
	}
}

// GetFiles retrieves the file details
var GetFiles = func(datasetID string) ([]*FileInfo, error) {
	var (
		r     []*FileInfo = nil
		err   error       = nil
		count             = 0
	)

	for count < dbRetryTimes {
		r, err = DB.getFiles(datasetID)
		if err != nil {
			count++

			continue
		}

		break
	}

	return r, err
}

// removeUserIDPrefix strips the user id prefix from a file path
func removeUserIDPrefix(filePath, userID string) string {
	strings.ReplaceAll(userID, "@", "_")
	// Construct the full prefix we expect to find (userID + "/").
	fullPrefix := userID + "/"
	if strings.HasPrefix(filePath, fullPrefix) {
		return strings.TrimPrefix(filePath, fullPrefix)
	}

	return filePath
}

// getFiles is the actual function performing work for GetFile
func (dbs *SQLdb) getFiles(datasetID string) ([]*FileInfo, error) {
	dbs.checkAndReconnectIfNeeded()

	files := []*FileInfo{}
	db := dbs.DB

	const query = `
SELECT files.stable_id AS id,
	reverse(split_part(reverse(files.submission_file_path::text), '/'::text, 1)) AS display_file_name,
	files.submission_user AS user_id,
	files.submission_file_path AS file_path,
	files.decrypted_file_size,
	sha_unenc.checksum AS decrypted_file_checksum,
	sha_unenc.type AS decrypted_file_checksum_type
FROM sda.files
 	JOIN sda.file_dataset file_dataset ON file_dataset.file_id = files.id
 	JOIN sda.datasets datasets ON file_dataset.dataset_id = datasets.id
	LEFT JOIN sda.checksums sha_unenc ON files.id = sha_unenc.file_id AND sha_unenc.source = 'UNENCRYPTED'
WHERE datasets.stable_id = $1;`

	// nolint:rowserrcheck
	rows, err := db.Query(query, datasetID)
	if err != nil {
		log.Error(err)

		return nil, err
	}
	defer rows.Close()

	var userID string

	// Iterate rows
	for rows.Next() {
		// Read rows into struct
		fi := &FileInfo{}
		err := rows.Scan(&fi.FileID, &fi.DisplayFileName,
			&userID, &fi.FilePath,
			&fi.DecryptedFileSize, &fi.DecryptedFileChecksum, &fi.DecryptedFileChecksumType)
		if err != nil {
			log.Error(err)

			return nil, err
		}

		// Process file info so that we don't leak any unneccessary info.
		fi.FilePath = removeUserIDPrefix(fi.FilePath, userID)

		// Add structs to array
		files = append(files, fi)
	}

	return files, nil
}

// CheckDataset checks if dataset name exists
var CheckDataset = func(dataset string) (bool, error) {
	var (
		r           = false
		err   error = nil
		count       = 0
	)

	for count < dbRetryTimes {
		r, err = DB.checkDataset(dataset)
		if err != nil {
			count++

			continue
		}

		break
	}

	return r, err
}

// checkDataset is the actual function performing work for CheckDataset
func (dbs *SQLdb) checkDataset(dataset string) (bool, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT stable_id FROM sda.datasets WHERE stable_id = $1;"

	var datasetName string
	if err := db.QueryRow(query, dataset).Scan(&datasetName); err != nil {
		return false, err
	}

	return true, nil
}

// GetDatasetInfo returns further information on a given `datasetID` as
// `*DatasetInfo`.
var GetDatasetInfo = func(datasetID string) (*DatasetInfo, error) {
	var (
		d     *DatasetInfo = nil
		err   error        = nil
		count              = 0
	)

	for count < dbRetryTimes {
		d, err = DB.getDatasetInfo(datasetID)
		if err != nil {
			count++

			continue
		}

		break
	}

	return d, err
}

func (dbs *SQLdb) getDatasetInfo(datasetID string) (*DatasetInfo, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT stable_id, created_at FROM sda.datasets WHERE stable_id = $1"

	dataset := &DatasetInfo{}
	if err := db.QueryRow(query, datasetID).Scan(&dataset.DatasetID, &dataset.CreatedAt); err != nil {
		return nil, err
	}

	return dataset, nil
}

// GetDatasetFileStableID returns the stable id of a file given a dataset ID and an
// upload file path
var GetDatasetFileStableID = func(datasetID, filePath string) (string, error) {
	var (
		fileStableID string
		err          error
		count        int
	)

	for count < dbRetryTimes {
		fileStableID, err = DB.getDatasetFileStableID(datasetID, filePath)
		if err != nil {
			count++

			continue
		}

		break
	}

	return fileStableID, err
}

// getDatasetFileInfo is the actual function performing work for GetFile
func (dbs *SQLdb) getDatasetFileStableID(datasetID, filePath string) (string, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB

	const query = `
SELECT files.stable_id
FROM sda.files
 	JOIN sda.file_dataset file_dataset ON file_dataset.file_id = files.id
 	JOIN sda.datasets datasets ON file_dataset.dataset_id = datasets.id
	WHERE datasets.stable_id = $1 AND files.submission_file_path ~ ('^[^/]*/?' || $2);`
	// regexp matching in the submission file path in order to disregard the
	// first slash-separated path element. The first path element is the id of
	// the uploading user which should not be displayed.

	var fileStableID string
	// nolint:rowserrcheck
	err := db.QueryRow(query, datasetID, filePath).Scan(&fileStableID)

	if err != nil {
		log.Error(err)

		return "", err
	}

	return fileStableID, nil
}

// CheckFilePermission checks if user has permissions to access the dataset the file is a part of
var CheckFilePermission = func(fileID string) (string, error) {
	var (
		r           = ""
		err   error = nil
		count       = 0
	)

	for count < dbRetryTimes {
		r, err = DB.checkFilePermission(fileID)
		if err != nil {
			count++

			continue
		}

		break
	}

	return r, err
}

// checkFilePermission is the actual function performing work for CheckFilePermission
func (dbs *SQLdb) checkFilePermission(fileID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()

	log.Debugf("check permissions for file with %s", sanitizeString(fileID))

	db := dbs.DB
	const query = `
		SELECT datasets.stable_id FROM sda.file_dataset
		JOIN sda.datasets ON dataset_id = datasets.id
		JOIN sda.files ON file_id = files.id
		WHERE files.stable_id = $1;
	`

	var datasetName string
	if err := db.QueryRow(query, fileID).Scan(&datasetName); err != nil {
		log.Errorf("requested file with %s does not exist", sanitizeString(fileID))

		return "", err
	}

	return datasetName, nil
}

// FileDownload details are used for downloading a file
type FileDownload struct {
	ArchivePath       string
	ArchiveSize       int
	DecryptedSize     int
	DecryptedChecksum string
	LastModified      string
	Header            []byte
}

// GetFile retrieves the file header
var GetFile = func(fileID string) (*FileDownload, error) {
	var (
		r     *FileDownload = nil
		err   error         = nil
		count               = 0
	)
	for count < dbRetryTimes {
		r, err = DB.getFile(fileID)
		if err != nil {
			count++

			continue
		}

		break
	}

	return r, err
}

// getFile is the actual function performing work for GetFile
func (dbs *SQLdb) getFile(fileID string) (*FileDownload, error) {
	dbs.checkAndReconnectIfNeeded()

	log.Debugf("check details for file with %s", sanitizeString(fileID))

	db := dbs.DB
	const query = `
		SELECT f.archive_file_path,
			   f.archive_file_size,
			   f.decrypted_file_size,
			   dc.checksum AS decrypted_checksum,
			   f.last_modified,
			   f.header
		FROM sda.files f
		LEFT JOIN (SELECT file_id, checksum, type
			FROM sda.checksums
		WHERE source = 'UNENCRYPTED') dc
		ON f.id = dc.file_id
		WHERE stable_id = $1`

	fd := &FileDownload{}
	var hexString string
	err := db.QueryRow(query, fileID).Scan(&fd.ArchivePath, &fd.ArchiveSize,
		&fd.DecryptedSize, &fd.DecryptedChecksum, &fd.LastModified, &hexString)
	if err != nil {
		log.Errorf("could not retrieve details for file %s, reason %s", sanitizeString(fileID), err)

		return nil, err
	}

	fd.Header, err = hex.DecodeString(hexString)
	if err != nil {
		log.Errorf("could not decode file header for file %s, reason %s", sanitizeString(fileID), err)

		return nil, err
	}

	return fd, nil
}

// Close terminates the connection to the database
func (dbs *SQLdb) Close() {
	db := dbs.DB
	_ = db.Close()
}
