package database

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/stretchr/testify/assert"
)

var testPgconf config.DatabaseConfig = config.DatabaseConfig{
	Host:       "localhost",
	Port:       42,
	User:       "user",
	Password:   "password",
	Database:   "database",
	CACert:     "cacert",
	SslMode:    "verify-full",
	ClientCert: "clientcert",
	ClientKey:  "clientkey",
}

const testConnInfo = "host=localhost port=42 user=user password=password dbname=database sslmode=verify-full sslrootcert=cacert sslcert=clientcert sslkey=clientkey"

func TestMain(m *testing.M) {
	// Set up our helper doing panic instead of os.exit
	logFatalf = testLogFatalf
	dbRetryTimes = 0
	dbReconnectTimeout = 200 * time.Millisecond
	dbReconnectSleep = time.Millisecond
	code := m.Run()

	os.Exit(code)
}

func TestBuildConnInfo(t *testing.T) {

	s := buildConnInfo(testPgconf)

	assert.Equalf(t, s, testConnInfo, "Bad string for verify-full: '%s' while expecting '%s'", s, testConnInfo)

	noSslConf := testPgconf
	noSslConf.SslMode = "disable"

	s = buildConnInfo(noSslConf)

	assert.Equalf(t, s,
		"host=localhost port=42 user=user password=password dbname=database sslmode=disable",
		"Bad string for disable: %s", s)

}

// testLogFatalf
func testLogFatalf(f string, args ...interface{}) {
	s := fmt.Sprintf(f, args...)
	panic(s)
}

func TestCheckAndReconnect(t *testing.T) {

	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))

	mock.ExpectPing().WillReturnError(fmt.Errorf("ping fail for testing bad conn"))

	err := CatchPanicCheckAndReconnect(SQLdb{db, ""})
	assert.Error(t, err, "Should have received error from checkAndReconnectOnNeeded fataling")

}

func CatchPanicCheckAndReconnect(db SQLdb) (err error) {
	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("Caught panic")
		}
	}()

	db.checkAndReconnectIfNeeded()

	return nil
}

func CatchNewDBPanic() (err error) {
	// Recover if NewDB panics
	// Allow both panic and error return here, so use a custom function rather
	// than assert.Panics

	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("Caught panic")
		}
	}()

	_, err = NewDB(testPgconf)

	return err
}

func TestNewDB(t *testing.T) {

	// Test failure first

	sqlOpen = func(x string, y string) (*sql.DB, error) {
		return nil, errors.New("fail for testing")
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)

	err := CatchNewDBPanic()

	if err == nil {
		t.Errorf("NewDB did not report error when it should.")
	}

	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))

	sqlOpen = func(dbName string, connInfo string) (*sql.DB, error) {
		if !assert.Equalf(t, dbName, "postgres",
			"Unexpected database name '%s' while expecting 'postgres'",
			dbName) {
			return nil, fmt.Errorf("Unexpected dbName %s", dbName)
		}

		if !assert.Equalf(t, connInfo, testConnInfo,
			"Unexpected connection info '%s' while expecting '%s",
			connInfo,
			testConnInfo) {
			return nil, fmt.Errorf("Unexpected connInfo %s", connInfo)
		}

		return db, nil
	}

	mock.ExpectPing().WillReturnError(fmt.Errorf("ping fail for testing"))

	err = CatchNewDBPanic()

	assert.NotNilf(t, err, "DB failed: %s", err)

	log.SetOutput(os.Stdout)

	assert.NotNil(t, err, "NewDB should fail when ping fails")

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

	mock.ExpectPing()
	_, err = NewDB(testPgconf)

	assert.Nilf(t, err, "NewDB failed unexpectedly: %s", err)

	err = mock.ExpectationsWereMet()
	assert.Nilf(t, err, "there were unfulfilled expectations: %s", err)

}

// Helper function for "simple" sql tests
func sqlTesterHelper(t *testing.T, f func(sqlmock.Sqlmock, *SQLdb) error) error {
	db, mock, err := sqlmock.New()

	sqlOpen = func(_ string, _ string) (*sql.DB, error) {
		return db, err
	}

	testDb, err := NewDB(testPgconf)

	assert.Nil(t, err, "NewDB failed unexpectedly")

	returnErr := f(mock, testDb)
	err = mock.ExpectationsWereMet()

	assert.Nilf(t, err, "there were unfulfilled expectations: %s", err)

	return returnErr
}

func TestClose(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectClose()
		testDb.Close()

		return nil
	})

	assert.Nil(t, r, "Close failed unexpectedly")
}

func TestCheckFilePermission(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		expected := "dataset1"
		query := "SELECT dataset_id FROM local_ega_ebi.file_dataset WHERE file_id = \\$1"
		mock.ExpectQuery(query).
			WithArgs("file1").
			WillReturnRows(sqlmock.NewRows([]string{"dataset_id"}).AddRow("dataset1"))

		x, err := testDb.checkFilePermission("file1")

		assert.Equal(t, expected, x, "did not get expected permission")

		return err
	})

	assert.Nil(t, r, "checkFilePermission failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestCheckDataset(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		expected := true
		query := "SELECT DISTINCT dataset_id FROM local_ega_ebi.file_dataset WHERE dataset_id = \\$1"
		mock.ExpectQuery(query).
			WithArgs("dataset1").
			WillReturnRows(sqlmock.NewRows([]string{"dataset_stable_id"}).AddRow("dataset1"))

		x, err := testDb.checkDataset("dataset1")

		assert.Equal(t, expected, x, "did not get expected dataset value")

		return err
	})

	assert.Nil(t, r, "checkDataset failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestGetFile(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		expected := &FileDownload{
			ArchivePath: "file.txt",
			ArchiveSize: 32,
			Header:      []byte{171, 193, 35},
		}
		query := "SELECT file_path, archive_file_size, header FROM local_ega_ebi.file WHERE file_id = \\$1"
		mock.ExpectQuery(query).
			WithArgs("file1").
			WillReturnRows(sqlmock.NewRows([]string{"file_path", "archive_file_size", "header"}).AddRow("file.txt", 32, "abc123"))

		x, err := testDb.getFile("file1")
		assert.Equal(t, expected, x, "did not get expected file details")

		return err
	})

	assert.Nil(t, r, "getFile failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestGetFiles(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		expected := []*FileInfo{}
		fileInfo := &FileInfo{
			FileID:                    "file1",
			DatasetID:                 "dataset1",
			DisplayFileName:           "file.txt",
			FileName:                  "urn:file1",
			FileSize:                  60,
			DecryptedFileSize:         32,
			DecryptedFileChecksum:     "hash",
			DecryptedFileChecksumType: "sha256",
			Status:                    "READY",
		}
		expected = append(expected, fileInfo)
		query := "SELECT a.file_id, dataset_id, display_file_name, file_name, file_size, " +
			"decrypted_file_size, decrypted_file_checksum, decrypted_file_checksum_type, file_status from " +
			"local_ega_ebi.file a, local_ega_ebi.file_dataset b WHERE dataset_id = \\$1 AND a.file_id=b.file_id;"
		mock.ExpectQuery(query).
			WithArgs("dataset1").
			WillReturnRows(sqlmock.NewRows([]string{"file_id", "dataset_id", "display_file_name",
				"file_name", "file_size", "decrypted_file_size", "decrypted_file_checksum", "decrypted_file_checksum_type",
				"file_status"}).AddRow("file1", "dataset1", "file.txt", "urn:file1", 60, 32, "hash", "sha256", "READY"))

		x, err := testDb.getFiles("dataset1")
		assert.Equal(t, expected, x, "did not get expected file details")

		return err
	})

	assert.Nil(t, r, "getFiles failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}
