package database

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
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

// FileInfo is returned by the metadata endpoint
type FileInfo struct {
	FileID                    string `json:"fileId"`
	DatasetID                 string `json:"datasetId"`
	DisplayFileName           string `json:"displayFileName"`
	FileName                  string `json:"fileName"`
	FileSize                  int64  `json:"fileSize"`
	DecryptedFileSize         int64  `json:"decryptedFileSize"`
	DecryptedFileChecksum     string `json:"decryptedFileChecksum"`
	DecryptedFileChecksumType string `json:"decryptedFileChecksumType"`
	Status                    string `json:"fileStatus"`
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
func NewDB(config config.DatabaseConfig) (*SQLdb, error) {
	connInfo := buildConnInfo(config)

	log.Debugf("Connecting to DB %s:%d on database: %s with user: %s", config.Host, config.Port, config.Database, config.User)
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
func buildConnInfo(config config.DatabaseConfig) string {
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

// GetFiles retrieves the file details
var GetFiles = func(datasetID string) ([]*FileInfo, error) {
	var (
		r     []*FileInfo = nil
		err   error       = nil
		count int         = 0
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

// getFiles is the actual function performing work for GetFile
func (dbs *SQLdb) getFiles(datasetID string) ([]*FileInfo, error) {
	dbs.checkAndReconnectIfNeeded()

	files := []*FileInfo{}
	db := dbs.DB

	const query = "SELECT a.file_id, dataset_id, display_file_name, file_name, file_size, " +
		"decrypted_file_size, decrypted_file_checksum, decrypted_file_checksum_type, file_status from " +
		"local_ega_ebi.file a, local_ega_ebi.file_dataset b WHERE dataset_id = $1 AND a.file_id=b.file_id;"

	// nolint:rowserrcheck
	rows, err := db.Query(query, datasetID)
	if err != nil {
		log.Error(err)

		return nil, err
	}
	defer rows.Close()

	// Iterate rows
	for rows.Next() {

		// Read rows into struct
		fi := &FileInfo{}
		err := rows.Scan(&fi.FileID, &fi.DatasetID, &fi.DisplayFileName, &fi.FileName, &fi.FileSize,
			&fi.DecryptedFileSize, &fi.DecryptedFileChecksum, &fi.DecryptedFileChecksumType, &fi.Status)
		if err != nil {
			log.Error(err)

			return nil, err
		}

		// NOTE FOR ENCRYPTED DOWNLOAD
		// As of now, encrypted download is not supported. When implementing encrypted download, note that
		// local_ega_ebi.file:file_size is the size of the file body in the archive without the header,
		// so the user needs to know the size of the header when downloading in encrypted format.
		// A way to get this could be:
		// fd := GetFile()
		// fi.FileSize = fi.FileSize + len(fd.Header)
		// But if the header is re-encrypted or a completely new header is generated, the length
		// needs to be conveyd to the user in some other way.

		// Add structs to array
		files = append(files, fi)
	}

	return files, nil
}

// CheckDataset checks if dataset name exists
var CheckDataset = func(dataset string) (bool, error) {
	var (
		r     bool  = false
		err   error = nil
		count int   = 0
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
	const query = "SELECT DISTINCT dataset_id FROM local_ega_ebi.file_dataset WHERE dataset_id = $1"

	var datasetName string
	if err := db.QueryRow(query, dataset).Scan(&datasetName); err != nil {
		return false, err
	}

	return true, nil
}

// CheckFilePermission checks if user has permissions to access the dataset the file is a part of
var CheckFilePermission = func(fileID string) (string, error) {
	var (
		r     string = ""
		err   error  = nil
		count int    = 0
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
	const query = "SELECT dataset_id FROM local_ega_ebi.file_dataset WHERE file_id = $1"

	var datasetName string
	if err := db.QueryRow(query, fileID).Scan(&datasetName); err != nil {
		log.Errorf("requested file with %s does not exist", sanitizeString(fileID))

		return "", err
	}

	return datasetName, nil
}

// FileDownload details are used for downloading a file
type FileDownload struct {
	ArchivePath string
	ArchiveSize int
	Header      []byte
}

// GetFile retrieves the file header
var GetFile = func(fileID string) (*FileDownload, error) {
	var (
		r     *FileDownload = nil
		err   error         = nil
		count int           = 0
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
	const query = "SELECT file_path, archive_file_size, header FROM local_ega_ebi.file WHERE file_id = $1"

	fd := &FileDownload{}
	var hexString string
	err := db.QueryRow(query, fileID).Scan(&fd.ArchivePath, &fd.ArchiveSize, &hexString)
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
	db.Close()
}
