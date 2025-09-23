package database

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
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
func testLogFatalf(f string, args ...any) {
	s := fmt.Sprintf(f, args...)
	panic(s)
}

func TestCheckAndReconnect(t *testing.T) {
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))

	mock.ExpectPing().WillReturnError(errors.New("ping fail for testing bad conn"))

	err := CatchPanicCheckAndReconnect(SQLdb{db, ""})
	assert.Error(t, err, "Should have received error from checkAndReconnectOnNeeded fataling")
}

func CatchPanicCheckAndReconnect(db SQLdb) (err error) {
	defer func() {
		r := recover()
		if r != nil {
			err = errors.New("Caught panic")
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
			err = errors.New("Caught panic")
		}
	}()

	_, err = NewDB(testPgconf)

	return err
}

func TestNewDB(t *testing.T) {
	// Test failure first

	sqlOpen = func(_ string, _ string) (*sql.DB, error) {
		return nil, errors.New("fail for testing")
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)

	err := CatchNewDBPanic()

	if err == nil {
		t.Error("NewDB did not report error when it should.")
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

	mock.ExpectPing().WillReturnError(errors.New("ping fail for testing"))

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
		query := `
			SELECT datasets.stable_id FROM sda.file_dataset
			JOIN sda.datasets ON dataset_id = datasets.id
			JOIN sda.files ON file_id = files.id
			WHERE files.stable_id = \$1;
		`
		mock.ExpectQuery(strings.ReplaceAll(strings.ReplaceAll(query, "\t", ""), "\n", " ")).
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
		query := `SELECT stable_id FROM sda.datasets WHERE stable_id = \$1`
		mock.ExpectQuery(query).
			WithArgs("dataset1").
			WillReturnRows(sqlmock.NewRows([]string{"stable_id"}).AddRow("dataset1"))

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

func TestGetDatasetInfo(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		expected := &DatasetInfo{
			DatasetID: "dataset1",
			CreatedAt: "now",
		}
		query := `SELECT stable_id, created_at FROM sda.datasets WHERE stable_id = \$1`
		mock.ExpectQuery(query).
			WithArgs("dataset1").
			WillReturnRows(sqlmock.NewRows([]string{"stable_id", "created_at"}).AddRow(expected.DatasetID, expected.CreatedAt))

		x, err := testDb.getDatasetInfo("dataset1")

		assert.Equal(t, expected, x, "did not get expected dataset value")

		return err
	})

	assert.Nil(t, r, "checkDataset failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestGetDatasetFileInfo(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		expected := &FileInfo{
			FileID:                    "file1",
			DatasetID:                 "dataset1",
			DisplayFileName:           "file.txt",
			FilePath:                  "dir/file.txt",
			EncryptedFileSize:         60,
			EncryptedFileChecksum:     "hash",
			EncryptedFileChecksumType: "sha256",
			DecryptedFileSize:         32,
			DecryptedFileChecksum:     "hash",
			DecryptedFileChecksumType: "sha256",
		}
		userID := "user1"

		query := `
		SELECT f.stable_id AS file_id,
			d.stable_id AS dataset_id,
			reverse\(split_part\(reverse\(f.submission_file_path::text\), '/'::text, 1\)\) AS display_file_name,
			f.submission_user AS user_id,
			f.submission_file_path AS file_path,
			f.archive_file_size AS file_size,
			lef.archive_file_checksum AS encrypted_file_checksum,
			lef.archive_file_checksum_type AS encrypted_file_checksum_type,
			f.decrypted_file_size,
			dc.checksum AS decrypted_file_checksum,
			dc.type AS decrypted_file_checksum_type
		FROM sda.files f
		JOIN sda.file_dataset fd ON fd.file_id = f.id
		JOIN sda.datasets d ON fd.dataset_id = d.id
		LEFT JOIN local_ega.files lef ON f.stable_id = lef.stable_id
		LEFT JOIN \(SELECT file_id,
					\(ARRAY_AGG\(event ORDER BY started_at DESC\)\)\[1\] AS event
				FROM sda.file_event_log
				GROUP BY file_id\) e
		ON f.id = e.file_id
		LEFT JOIN \(SELECT file_id, checksum, type
			FROM sda.checksums
		WHERE source = 'UNENCRYPTED'\) dc
		ON f.id = dc.file_id
		WHERE d.stable_id = \$1 AND f.submission_file_path ~ \('\^\[\^\/\]\*/\?' \|\| \$2\);`
		mock.ExpectQuery(query).
			WithArgs("dataset1", "file1").
			WillReturnRows(sqlmock.NewRows([]string{"file_id", "dataset_id",
				"display_file_name", "user_id", "file_path", "file_size",
				"encrypted_file_checksum", "encrypted_file_checksum_type", "decrypted_file_size", "decrypted_file_checksum",
				"decrypted_file_checksum_type"}).AddRow(expected.FileID, expected.DatasetID,
				expected.DisplayFileName, userID, expected.FilePath,
				expected.EncryptedFileSize, expected.EncryptedFileChecksum, expected.EncryptedFileChecksumType, expected.DecryptedFileSize,
				expected.DecryptedFileChecksum, expected.DecryptedFileChecksumType))

		x, err := testDb.getDatasetFileInfo("dataset1", "file1")

		assert.Equal(t, expected, x, "did not get expected file values")

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
			ArchivePath:       "file.txt",
			ArchiveSize:       32,
			DecryptedSize:     1024,
			DecryptedChecksum: "sha256checksum",
			LastModified:      "now",
			Header:            []byte{171, 193, 35},
		}
		query := `
		SELECT f.archive_file_path,
			   f.archive_file_size,
			   f.decrypted_file_size,
			   dc.checksum AS decrypted_checksum,
			   f.last_modified,
			   f.header
		FROM sda.files f
		LEFT JOIN \(SELECT file_id, checksum, type
			FROM sda.checksums
		WHERE source = 'UNENCRYPTED'\) dc
		ON f.id = dc.file_id
		WHERE stable_id = \$1`

		mock.ExpectQuery(query).
			WithArgs("file1").
			WillReturnRows(sqlmock.NewRows([]string{"file_path", "archive_file_size",
				"decrypted_file_size", "decrypted_checksum", "last_modified", "header"}).AddRow(
				expected.ArchivePath, expected.ArchiveSize, expected.DecryptedSize,
				expected.DecryptedChecksum, expected.LastModified, "abc123"))

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
