package database

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

/*
 * Tests for database usage.
 *
 * We do not try to be smart but rather verify specific SQL being run from
 * the various functions.
 */

var testPgconf = DBConf{"localhost",
	42,
	"user",
	"password",
	"database",
	"cacert",
	"verify-full",
	"clientcert",
	"clientkey"}

const testConnInfo = "host=localhost port=42 user=user password=password dbname=database sslmode=verify-full sslrootcert=cacert sslcert=clientcert sslkey=clientkey"

func TestMain(m *testing.M) {
	// Set up our helper doing panic instead of os.exit
	logFatalf = testLogFatalf
	dbRetryTimes = 1
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

func TestMarkCompleted(t *testing.T) {
	file := FileInfo{sha256.New(), 46, "/somepath", sha256.New(), 48}

	_, err := file.Checksum.Write([]byte("checksum"))

	if err != nil {
		return
	}

	_, err = file.DecryptedChecksum.Write([]byte("decryptedchecksum"))

	if err != nil {
		return
	}

	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		r := sqlmock.NewResult(10, 1)

		mock.ExpectExec("UPDATE local_ega.files SET status = 'COMPLETED', "+
			"archive_filesize = \\$2, "+
			"archive_file_checksum = \\$3, "+
			"archive_file_checksum_type = \\$4, "+
			"decrypted_file_size = \\$5, "+
			"decrypted_file_checksum = \\$6, "+
			"decrypted_file_checksum_type = \\$7 "+
			"WHERE id = \\$1;").WithArgs(
			10,
			file.Size,
			"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
			"SHA256",
			file.DecryptedSize,
			"b353d3058b350466bb75a4e5e2263c73a7b900e2c48804780c6dd820b8b151ba",
			"SHA256").WillReturnResult(r)

		return testDb.MarkCompleted(file, 10)
	})

	assert.Nil(t, r, "MarkCompleted failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()
	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectExec("UPDATE local_ega.files SET status = 'COMPLETED', "+
			"archive_filesize = \\$2, "+
			"archive_file_checksum = \\$3, "+
			"archive_file_checksum_type = \\$4, "+
			"decrypted_file_size = \\$5, "+
			"decrypted_file_checksum = \\$6, "+
			"decrypted_file_checksum_type = \\$7 "+
			"WHERE id = \\$1;").
			WithArgs(10,
				file.Size,
				"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
				"SHA256",
				file.DecryptedSize,
				"b353d3058b350466bb75a4e5e2263c73a7b900e2c48804780c6dd820b8b151ba",
				"SHA256").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.MarkCompleted(file, 10)
	})

	assert.NotNil(t, r, "MarkCompleted did not fail as expected")

	log.SetOutput(os.Stdout)
}

func TestInsertFile(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectQuery("INSERT INTO local_ega.main\\(submission_file_path, submission_file_extension, submission_user, status, encryption_method\\) VALUES\\(\\$1, \\$2, \\$3,'INIT', 'CRYPT4GH'\\) RETURNING id;").
			WithArgs("/tmp/file.c4gh", "c4gh", "nobody").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))

		_, err := testDb.InsertFile("/tmp/file.c4gh", "nobody")

		return err
	})

	assert.Nil(t, r, "InsertFile failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestGetHeader(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		header := []byte{15, 64}
		mock.ExpectQuery("SELECT header from local_ega.files WHERE id = \\$1").
			WithArgs(42).
			WillReturnRows(sqlmock.NewRows([]string{"header"}).AddRow("0f40"))

		x, err := testDb.GetHeader(42)

		assert.Equal(t, x, header, "did not get expected header")

		return err
	})

	assert.Nil(t, r, "GetHeader failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestGetHeaderForStableID(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		header := "0f40"
		mock.ExpectQuery("SELECT header from local_ega.files WHERE stable_id = \\$1").
			WithArgs("42").
			WillReturnRows(sqlmock.NewRows([]string{"header"}).AddRow("0f40"))

		x, err := testDb.GetHeaderForStableID("42")

		assert.Equal(t, x, header, "did not get expected header")

		return err
	})

	assert.Nil(t, r, "GetHeaderForStableID failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestStoreHeader(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		header := []byte{15, 45, 20, 40, 48}
		r := sqlmock.NewResult(10, 1)

		mock.ExpectExec("UPDATE local_ega.files SET header = \\$1 WHERE id = \\$2;").
			WithArgs("0f2d142830", 42).
			WillReturnResult(r)

		return testDb.StoreHeader(header, 42)
	})

	assert.Nil(t, r, "StoreHeader failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	log.SetOutput(os.Stdout)
}

func TestSetArchived(t *testing.T) {

	file := FileInfo{sha256.New(), 1000, "/tmp/file.c4gh", sha256.New(), -1}
	_, err := file.Checksum.Write([]byte("checksum"))

	if err != nil {
		return
	}

	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		r := sqlmock.NewResult(10, 1)

		mock.ExpectExec("UPDATE local_ega.files SET status = 'ARCHIVED', archive_path = \\$1, archive_filesize = \\$2, inbox_file_checksum = \\$3, inbox_file_checksum_type = \\$4 WHERE id = \\$5;").
			WithArgs(file.Path,
				file.Size,
				"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
				"SHA256",
				42).
			WillReturnResult(r)

		return testDb.SetArchived(file, 42)
	})

	assert.Nil(t, r, "SetArchived failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	buf.Reset()

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectExec("UPDATE local_ega.files SET status = 'ARCHIVED', archive_path = \\$1, archive_filesize = \\$2, inbox_file_checksum = \\$3, inbox_file_checksum_type = \\$4 WHERE id = \\$5;").
			WithArgs(file.Path,
				file.Size,
				"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
				"SHA256", 42).
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.SetArchived(file, 42)
	})

	assert.NotNil(t, r, "SetArchived did not fail correctly")

	log.SetOutput(os.Stdout)
}

func TestMarkReady(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		r := sqlmock.NewResult(10, 1)

		mock.ExpectExec("UPDATE local_ega.files SET status = 'READY', "+
			"stable_id = \\$1 WHERE "+
			"elixir_id = \\$2 and "+
			"inbox_path = \\$3 and "+
			"decrypted_file_checksum = \\$4 and "+
			"status = 'COMPLETED';").
			WithArgs("accessionId", "nobody", "/tmp/file.c4gh", "checksum").
			WillReturnResult(r)

		return testDb.MarkReady("accessionId", "nobody", "/tmp/file.c4gh", "checksum")
	})

	assert.Nil(t, r, "MarkReady failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectExec("UPDATE local_ega.files SET status = 'READY', "+
			"stable_id = \\$1 WHERE "+
			"elixir_id = \\$2 and "+
			"inbox_path = \\$3 and "+
			"decrypted_file_checksum = \\$4 and "+
			"status = 'COMPLETED';").
			WithArgs("accessionId", "nobody", "/tmp/file.c4gh", "checksum").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.MarkReady("accessionId", "nobody", "/tmp/file.c4gh", "checksum")
	})

	assert.NotNil(t, r, "MarkReady did not fail as expected")

	log.SetOutput(os.Stdout)
}
func TestMapFilesToDataset(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		// Set up a few file sets with different accession ids.
		accessions := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}

		diSet := map[string][]string{"dataset1": accessions[0:3],
			"dataset2": accessions[4:5],
			"dataset3": accessions[6:8],
			"dataset4": accessions[9:9]}

		success := sqlmock.NewResult(1, 1)

		for di, acs := range diSet {
			mock.ExpectBegin()
			for _, aID := range acs {
				r := sqlmock.NewRows([]string{"file_id"})

				fileID, _ := strconv.Atoi(aID)

				r.AddRow(fileID)

				mock.ExpectQuery("SELECT file_id FROM local_ega.archive_files WHERE stable_id = \\$1").
					WithArgs(aID).WillReturnRows(r)

				mock.ExpectExec("INSERT INTO local_ega_ebi.filedataset "+
					"\\(file_id, dataset_stable_id\\) VALUES \\(\\$1, \\$2\\) "+
					"ON CONFLICT "+
					"DO NOTHING;").
					WithArgs(fileID, di).WillReturnResult(success)

			}
			mock.ExpectCommit()

			err := testDb.MapFilesToDataset(di, acs)

			if !assert.Nil(t, err, "MapFilesToDataset failed unexpectedly") {
				return err
			}
		}

		var buf bytes.Buffer
		log.SetOutput(&buf)

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT file_id FROM local_ega.archive_files WHERE stable_id = \\$1").
			WithArgs("aid1").WillReturnError(fmt.Errorf("error for testing"))
		mock.ExpectRollback().WillReturnError(fmt.Errorf("error again"))

		err := testDb.MapFilesToDataset("dataset", []string{"aid1"})

		assert.NotZero(t, buf.Len(), "Expected warning missing")
		assert.NotNil(t, err, "MapFilesToDataset did not fail as expected")

		buf.Reset()

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT file_id FROM local_ega.archive_files WHERE stable_id = \\$1").
			WithArgs("aid1").WillReturnRows(sqlmock.NewRows([]string{"file_id"}).AddRow(100))

		mock.ExpectExec("INSERT INTO local_ega_ebi.filedataset "+
			"\\(file_id, dataset_stable_id\\) "+
			"VALUES \\(\\$1, \\$2\\) "+
			"ON CONFLICT "+
			"DO NOTHING;").
			WithArgs(100, "dataset").WillReturnError(fmt.Errorf("error for testing"))

		mock.ExpectRollback().WillReturnError(fmt.Errorf("error again"))

		err = testDb.MapFilesToDataset("dataset", []string{"aid1"})

		assert.NotZero(t, buf.Len(), "Expected warning missing")
		assert.NotNil(t, err, "MapFilesToDataset did not fail as expected")

		log.SetOutput(os.Stdout)

		return nil
	})

	assert.Nil(t, r, "Tests for MapFilesToDataset failed unexpectedly")
}

func TestClose(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectClose()
		testDb.Close()

		return nil
	})

	assert.Nil(t, r, "Close failed unexpectedly")
}
