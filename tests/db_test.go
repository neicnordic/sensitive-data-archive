package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	_ "github.com/lib/pq"
)

// RandomString is used to create random test values
func RandomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

// SQLdb struct that acts as a receiver for the DB update methods
type TestDb struct {
	DB       *sql.DB
	ConnInfo string
	resetSQL []string
}

// DBConf stores information about the database backend
type DBConf struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// NewDB creates a new DB connection
func NewTestDb(config DBConf) (*TestDb, error) {
	connInfo := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable",
		config.Host, config.Port, config.User, config.Database)

	log.Debugf("Connecting to DB %s:%d on database: %s with user: %s",
		config.Host, config.Port, config.Database, config.User)
	db, err := sql.Open("postgres", connInfo)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	return &TestDb{DB: db, ConnInfo: connInfo}, nil
}

// ResetTestData reloads the suite.test_data to restore the database
func (dbs *TestDb) ResetTestData() {
	start := time.Now()

	for dbs.DB.Ping() != nil {
		log.Errorln("Database unreachable, reconnecting")
		dbs.DB.Close()

		if time.Since(start) > 1*time.Minute {
			log.Error("could not reconnect to failed database in reasonable time, giving up")
		}
		time.Sleep(5 * time.Second)
		log.Debugln("Reconnecting to DB")
		dbs.DB, _ = sql.Open("postgres", dbs.ConnInfo)
	}

	for _, query := range dbs.resetSQL {
		_, err := dbs.DB.Exec(query)
		if err != nil {
			log.Errorf("failed to execute reset sql: %s", err)
		}
	}

}

type DatabaseTests struct {
	suite.Suite
	db     *TestDb
	dbConf DBConf
}

func TestDatabaseTestSuite(t *testing.T) {
	suite.Run(t, new(DatabaseTests))
}

func (suite *DatabaseTests) SetupTest() {

	var err error

	// Connect to the database
	suite.dbConf = DBConf{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Database: "lega",
	}

	suite.db, err = NewTestDb(suite.dbConf)
	if err != nil {
		log.Errorf("could not connect to database: %s", err)
	}

	// load test data (read file and separate by semi-colon)
	rawSQL, err := ioutil.ReadFile("test_data.sql")
	if err != nil {
		log.Errorf("could not read reset SQL file: %s", err)
	}

	suite.db.resetSQL = strings.Split(string(rawSQL), ";")
}

func (suite *DatabaseTests) TearDownTest() {}

func (suite *DatabaseTests) TestLoadTestData() {

	suite.db.ResetTestData()

	var numFiles int
	err := suite.db.DB.QueryRow("SELECT COUNT(*) FROM sda.files;").Scan(&numFiles)
	assert.NoError(suite.T(), err)
	assert.Greater(suite.T(), numFiles, 0, "No file entries in test data")

}

func (suite *DatabaseTests) TestFilesUpdateTrigger() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// select random file from the database
	var fileID string
	var modString string
	err := db.QueryRow("SELECT id, last_modified FROM sda.files ORDER BY RANDOM() LIMIT 1;").Scan(&fileID, &modString)
	assert.NoError(suite.T(), err)

	// update entry
	_, err = db.Exec("UPDATE sda.files SET stable_id = 'update_test' WHERE id = $1;", fileID)
	assert.NoError(suite.T(), err)

	// check that the last_modified and last_modified_by fields have updated properly
	var updateString string
	var updatedBy string
	err = db.QueryRow("SELECT last_modified, last_modified_by FROM sda.files WHERE id = $1;", fileID).Scan(&updateString, &updatedBy)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), updatedBy, suite.dbConf.User, "Wrong user in updated_by field")

	initial, err := time.Parse("2006-01-02T15:04:05.999999999Z", modString)
	assert.NoError(suite.T(), err)
	updated, err := time.Parse("2006-01-02T15:04:05.999999999Z", updateString)
	assert.NoError(suite.T(), err)
	assert.Greater(suite.T(), updated, initial, "updated time is not greater than original")

}

// Tests of the legacy view schema.

func (suite *DatabaseTests) TestMainToFilesTrigger() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// insert data into sda.files, and see that a new row is created in
	// local_ega.main_to_files
	var fileID string
	err := db.QueryRow("INSERT INTO sda.files (submission_user) VALUES ('testuser') RETURNING id;").Scan(&fileID)
	assert.NoError(suite.T(), err)

	var mainID int64 = -1
	err = db.QueryRow("SELECT main_id FROM local_ega.main_to_files WHERE files_id = $1;", fileID).Scan(&mainID)
	assert.NoError(suite.T(), err)

	assert.Greater(suite.T(), mainID, int64(0), "main_to_files trigger failed")

}

func (suite *DatabaseTests) TestMainInsert() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// insert data into local_ega.main, and check that the values show up
	// correctly in both local_ega.main and the sda schema tables

	legalStatuses := []string{"INIT", "IN_INGESTION", "INGESTED", "ARCHIVED",
		"COMPLETED", "BACKED UP"}

	legalChecksums := []string{"MD5", "SHA256", "SHA384", "SHA512"}

	legalAlgorithms := []string{"CRYPT4GH"}

	// random inserted values
	stableID := RandomString(10)
	status := legalStatuses[rand.Intn(len(legalStatuses))]
	submissionFileExtension := RandomString(10)
	submissionFilePath := RandomString(10) + "." + submissionFileExtension
	submissionFileCalculatedChecksum := RandomString(32)
	submissionFileCalculatedChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	submissionFileSize := rand.Intn(1000000000)
	submissionUser := RandomString(10)
	archiveFileReference := RandomString(10)
	archiveFileSize := rand.Intn(1000000000)
	archiveFileChecksum := RandomString(32)
	archiveFileChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	decryptedFileSize := rand.Intn(1000000000)
	decryptedFileChecksum := RandomString(32)
	decryptedFileChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	encryptionMethod := legalAlgorithms[rand.Intn(len(legalAlgorithms))]
	version := 1
	header := RandomString(32)

	// id to return from insert
	var mainID int64

	fields := `stable_id,
	status,
	submission_file_path,
	submission_file_extension,
	submission_file_calculated_checksum,
	submission_file_calculated_checksum_type,
	submission_file_size,
	submission_user,
	archive_file_reference,
	archive_file_size,
	archive_file_checksum,
	archive_file_checksum_type,
	decrypted_file_size,
	decrypted_file_checksum,
	decrypted_file_checksum_type,
	encryption_method,
	version,
	header`

	err := db.QueryRow("INSERT INTO local_ega.main ("+
		fields+`
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18
		) RETURNING id;`, stableID, status, submissionFilePath,
		submissionFileExtension, submissionFileCalculatedChecksum,
		submissionFileCalculatedChecksumType, submissionFileSize,
		submissionUser, archiveFileReference, archiveFileSize,
		archiveFileChecksum, archiveFileChecksumType, decryptedFileSize,
		decryptedFileChecksum, decryptedFileChecksumType, encryptionMethod,
		version, header,
	).Scan(&mainID)
	assert.NoError(suite.T(), err)

	// Return values
	retStableID := ""
	retStatus := ""
	retSubmissionFilePath := ""
	retSubmissionFileExtension := ""
	retSubmissionFileCalculatedChecksum := ""
	retSubmissionFileCalculatedChecksumType := ""
	retSubmissionFileSize := -1
	retSubmissionUser := ""
	retArchiveFileReference := ""
	retArchiveFileSize := -1
	retArchiveFileChecksum := ""
	retArchiveFileChecksumType := ""
	retDecryptedFileSize := -1
	retDecryptedFileChecksum := ""
	retDecryptedFileChecksumType := ""
	retEncryptionMethod := ""
	retVersion := -1
	retHeader := ""

	err = db.QueryRow("SELECT "+fields+" FROM local_ega.main WHERE id = $1;", mainID).Scan(&retStableID, &retStatus, &retSubmissionFilePath,
		&retSubmissionFileExtension, &retSubmissionFileCalculatedChecksum,
		&retSubmissionFileCalculatedChecksumType, &retSubmissionFileSize,
		&retSubmissionUser, &retArchiveFileReference,
		&retArchiveFileSize, &retArchiveFileChecksum,
		&retArchiveFileChecksumType, &retDecryptedFileSize,
		&retDecryptedFileChecksum, &retDecryptedFileChecksumType,
		&retEncryptionMethod, &retVersion, &retHeader)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), stableID, retStableID, "StableID was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), strings.ToUpper(status), retStatus, "Status was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFilePath, retSubmissionFilePath, "SubmissionFilePath  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileExtension, retSubmissionFileExtension, "SubmissionFileExtension  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileCalculatedChecksum, retSubmissionFileCalculatedChecksum, "SubmissionFileCalculatedChecksum  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileCalculatedChecksumType, retSubmissionFileCalculatedChecksumType, "SubmissionFileCalculatedChecksumType  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileSize, retSubmissionFileSize, "SubmissionFileSize  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), submissionUser, retSubmissionUser, "SubmissionUser  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileReference, retArchiveFileReference, "ArchiveFileReference  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileSize, retArchiveFileSize, "ArchiveFileSize  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileChecksum, retArchiveFileChecksum, "ArchiveFileChecksum  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileChecksumType, retArchiveFileChecksumType, "ArchiveFileChecksumType  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileSize, retDecryptedFileSize, "DecryptedFileSize  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileChecksum, retDecryptedFileChecksum, "DecryptedFileChecksum  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileChecksumType, retDecryptedFileChecksumType, "DecryptedFileChecksumType  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), encryptionMethod, retEncryptionMethod, "EncryptionMethod  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), version, retVersion, "Version  was not inserted correctly into local_ega.main")
	assert.Equal(suite.T(), header, retHeader, "Header  was not inserted correctly into local_ega.main")
}

func (suite *DatabaseTests) TestMainUpdate() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// update the test data for "testaccession01" with random variables

	legalStatuses := []string{"INIT", "IN_INGESTION", "ingested", "archived",
		"COMPLETED", "backed up"}

	legalChecksums := []string{"MD5", "SHA256", "SHA384", "SHA512"}

	legalAlgorithms := []string{"CRYPT4GH", "PGP", "AES"}

	// random inserted values
	stableID := RandomString(10)
	status := legalStatuses[rand.Intn(len(legalStatuses))]
	submissionFileExtension := RandomString(10)
	submissionFilePath := RandomString(10) + "." + submissionFileExtension
	submissionFileCalculatedChecksum := RandomString(32)
	submissionFileCalculatedChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	submissionFileSize := rand.Intn(1000000000)
	submissionUser := RandomString(10)
	archiveFileReference := RandomString(10)
	archiveFileSize := rand.Intn(1000000000)
	archiveFileChecksum := RandomString(32)
	archiveFileChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	decryptedFileSize := rand.Intn(1000000000)
	decryptedFileChecksum := RandomString(32)
	decryptedFileChecksumType := legalChecksums[rand.Intn(len(legalChecksums))]
	encryptionMethod := legalAlgorithms[rand.Intn(len(legalAlgorithms))]
	header := RandomString(32)

	// id to return from insert
	var mainID int64

	err := db.QueryRow(`UPDATE local_ega.main SET
		stable_id = $1,
		status = $2,
		submission_file_path = $3,
		submission_file_extension = $4,
		submission_file_calculated_checksum = $5,
		submission_file_calculated_checksum_type = $6,
		submission_file_size = $7,
		submission_user = $8,
		archive_file_reference = $9,
		archive_file_size = $10,
		archive_file_checksum = $11,
		archive_file_checksum_type = $12,
		decrypted_file_size = $13,
		decrypted_file_checksum = $14,
		decrypted_file_checksum_type = $15,
		encryption_method = $16,
		header = $17
		WHERE stable_id = 'testaccession01' RETURNING id;`, stableID, status, submissionFilePath,
		submissionFileExtension, submissionFileCalculatedChecksum,
		submissionFileCalculatedChecksumType, submissionFileSize,
		submissionUser, archiveFileReference, archiveFileSize,
		archiveFileChecksum, archiveFileChecksumType, decryptedFileSize,
		decryptedFileChecksum, decryptedFileChecksumType, encryptionMethod,
		header,
	).Scan(&mainID)
	assert.NoError(suite.T(), err)

	// Return values
	retStableID := ""
	retStatus := ""
	retSubmissionFilePath := ""
	retSubmissionFileExtension := ""
	retSubmissionFileCalculatedChecksum := ""
	retSubmissionFileCalculatedChecksumType := ""
	retSubmissionFileSize := -1
	retSubmissionUser := ""
	retArchiveFileReference := ""
	retArchiveFileSize := -1
	retArchiveFileChecksum := ""
	retArchiveFileChecksumType := ""
	retDecryptedFileSize := -1
	retDecryptedFileChecksum := ""
	retDecryptedFileChecksumType := ""
	retEncryptionMethod := ""
	retVersion := -1
	retHeader := ""

	fields := `stable_id,
	status,
	submission_file_path,
	submission_file_extension,
	submission_file_calculated_checksum,
	submission_file_calculated_checksum_type,
	submission_file_size,
	submission_user,
	archive_file_reference,
	archive_file_size,
	archive_file_checksum,
	archive_file_checksum_type,
	decrypted_file_size,
	decrypted_file_checksum,
	decrypted_file_checksum_type,
	encryption_method,
	version,
	header`

	err = db.QueryRow("SELECT "+fields+" FROM local_ega.main WHERE id = $1;", mainID).Scan(&retStableID, &retStatus, &retSubmissionFilePath,
		&retSubmissionFileExtension, &retSubmissionFileCalculatedChecksum,
		&retSubmissionFileCalculatedChecksumType, &retSubmissionFileSize,
		&retSubmissionUser, &retArchiveFileReference,
		&retArchiveFileSize, &retArchiveFileChecksum,
		&retArchiveFileChecksumType, &retDecryptedFileSize,
		&retDecryptedFileChecksum, &retDecryptedFileChecksumType,
		&retEncryptionMethod, &retVersion, &retHeader)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), stableID, retStableID, "StableID was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), strings.ToUpper(status), retStatus, "Status  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFilePath, retSubmissionFilePath, "SubmissionFilePath  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileExtension, retSubmissionFileExtension, "SubmissionFileExtension  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileCalculatedChecksum, retSubmissionFileCalculatedChecksum, "SubmissionFileCalculatedChecksum  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileCalculatedChecksumType, retSubmissionFileCalculatedChecksumType, "SubmissionFileCalculatedChecksumType  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionFileSize, retSubmissionFileSize, "SubmissionFileSize  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), submissionUser, retSubmissionUser, "SubmissionUser  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileReference, retArchiveFileReference, "ArchiveFileReference  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileSize, retArchiveFileSize, "ArchiveFileSize  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileChecksum, retArchiveFileChecksum, "ArchiveFileChecksum  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), archiveFileChecksumType, retArchiveFileChecksumType, "ArchiveFileChecksumType  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileSize, retDecryptedFileSize, "DecryptedFileSize  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileChecksum, retDecryptedFileChecksum, "DecryptedFileChecksum  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), decryptedFileChecksumType, retDecryptedFileChecksumType, "DecryptedFileChecksumType  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), encryptionMethod, retEncryptionMethod, "EncryptionMethod  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), 1, retVersion, "Version  was not updated correctly into local_ega.main")
	assert.Equal(suite.T(), header, retHeader, "Header  was not updated correctly into local_ega.main")
}

func (suite *DatabaseTests) TestLocalEGAInsertFile() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// random inserted values
	path := RandomString(10)
	extension := RandomString(4)
	path = path + "." + extension
	userID := RandomString(10)

	// id to return from function
	var mainID int64

	err := db.QueryRow("SELECT local_ega.insert_file ($1, $2);", path, userID).Scan(&mainID)
	assert.NoError(suite.T(), err)

	retPath := ""
	retExt := ""
	retUser := ""
	retStatus := ""

	err = db.QueryRow(`SELECT
			submission_file_path,
			submission_file_extension,
			submission_user,
			status
			FROM local_ega.main WHERE id = $1;`, mainID).Scan(&retPath, &retExt, &retUser, &retStatus)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), path, retPath, "submission_file_path was not inserted correctly by local_ega.insert_file")
	assert.Equal(suite.T(), extension, retExt, "submission_file_extension was not inserted correctly into local_ega.insert_file")
	assert.Equal(suite.T(), userID, retUser, "submission_file_user was not inserted correctly into local_ega.insert_file")
	assert.Equal(suite.T(), "INIT", retStatus, "status was not inserted correctly into local_ega.insert_file")
}

func (suite *DatabaseTests) TestLocalEGAFinalizeFile() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// get values for the finalize_file function.
	inpath := ""
	eid := ""
	checksum := ""
	checksumType := ""
	sid := ""

	stableID := "testaccession01"

	err := db.QueryRow(`SELECT
			submission_file_path,
			submission_user,
			archive_file_checksum,
			archive_file_checksum_type,
			stable_id
			FROM local_ega.main WHERE stable_id = $1;`, stableID,
	).Scan(&inpath, &eid, &checksum, &checksumType, &sid)
	assert.NoError(suite.T(), err)

	_, err = db.Exec("SELECT local_ega.finalize_file ($1, $2, $3, $4, $5);",
		inpath, eid, checksum, checksumType, sid)
	assert.NoError(suite.T(), err)

	status := ""

	err = db.QueryRow(`SELECT status FROM local_ega.main WHERE stable_id = $1;`,
		stableID).Scan(&status)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), "READY", status, "local_ega.finalize_file did not correctly mark file as ready")
}

func (suite *DatabaseTests) TestLocalEGAIsDisabled() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// get the main ID values from the database

	mainIDone := -1
	mainIDtwo := -1

	err := db.QueryRow(`SELECT id FROM local_ega.main WHERE stable_id = 'testaccession01';`).Scan(&mainIDone)
	assert.NoError(suite.T(), err)

	err = db.QueryRow(`SELECT id FROM local_ega.main WHERE stable_id = 'testaccession02';`).Scan(&mainIDtwo)
	assert.NoError(suite.T(), err)

	// Check that the first is enabled and the other disabled

	var isDisabled bool

	err = db.QueryRow("SELECT local_ega.is_disabled ($1);", mainIDone).Scan(&isDisabled)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), false, isDisabled, "local_ega.is_disabled falsely reported testaccession01 as disabled")

	err = db.QueryRow("SELECT local_ega.is_disabled ($1);", mainIDtwo).Scan(&isDisabled)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), true, isDisabled, "local_ega.is_disabled falsely reported testaccession02 as enabled")
}

func (suite *DatabaseTests) TestLocalEGAInsertError() {

	// Reset data, and set convenience db variable
	suite.db.ResetTestData()
	db := suite.db.DB

	// get main ID values from the database

	mainID := -1

	err := db.QueryRow(`SELECT id FROM local_ega.main WHERE stable_id = 'testaccession01';`).Scan(&mainID)
	assert.NoError(suite.T(), err)

	// random variables

	hostname := RandomString(10)
	etype := RandomString(10)
	user := RandomString(10)
	msg := RandomString(10)

	// Insert error

	_, err = db.Exec(`SELECT local_ega.insert_error($1, $2, $3, $4, $5);`, mainID, hostname, etype, msg, user)
	assert.NoError(suite.T(), err)

	// check that the error is in the error view

	retHostname := ""
	retEType := ""
	retUser := ""
	retMsg := ""

	err = db.QueryRow(`SELECT hostname, error_type, from_user, msg FROM local_ega.main_errors WHERE file_id = $1`,
		mainID).Scan(&retHostname, &retEType, &retUser, &retMsg)
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), hostname, retHostname, "local_ega.insert_error did not correctly insert hostname")
	assert.Equal(suite.T(), etype, retEType, "local_ega.insert_error did not correctly insert error_type")
	assert.Equal(suite.T(), user, retUser, "local_ega.insert_error did not correctly insert from_user")
	assert.Equal(suite.T(), msg, retMsg, "local_ega.insert_error did not correctly insert msg")
}
