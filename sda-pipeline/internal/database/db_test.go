package database

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
		r := sqlmock.NewResult(0, 1)
		mock.ExpectExec("SELECT sda.set_verified\\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\);").
			WithArgs(
				"fb140b10-1354-4266-879e-b34ad3e64c57",
				"71bb2f05-2061-41ac-9f62-32322fde7e7d",
				"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
				"SHA256",
				file.DecryptedSize,
				"b353d3058b350466bb75a4e5e2263c73a7b900e2c48804780c6dd820b8b151ba",
				"SHA256",
			).WillReturnResult(r)

		return testDb.MarkCompleted(file, "fb140b10-1354-4266-879e-b34ad3e64c57", "71bb2f05-2061-41ac-9f62-32322fde7e7d")
	})
	assert.Nil(t, r, "MarkCompleted failed unexpectedly")

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectExec("SELECT sda.set_verified\\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\);").
			WithArgs(
				"fb140b10-1354-4266-879e-b34ad3e64c57",
				"71bb2f05-2061-41ac-9f62-32322fde7e7d",
				"96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b",
				"SHA256",
				file.DecryptedSize,
				"b353d3058b350466bb75a4e5e2263c73a7b900e2c48804780c6dd820b8b151ba",
				"SHA256",
			).WillReturnError(fmt.Errorf("error for testing"))

		return testDb.MarkCompleted(file, "fb140b10-1354-4266-879e-b34ad3e64c57", "71bb2f05-2061-41ac-9f62-32322fde7e7d")
	})
	assert.NotNil(t, r, "MarkCompleted did not fail as expected")
}

func TestRegisterFile(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectQuery("SELECT sda.register_file\\(\\$1, \\$2\\);").
			WithArgs("tmp/file1.c4gh", "dummy").
			WillReturnRows(sqlmock.NewRows([]string{"register_file"}).AddRow("074803cc-718e-4dc4-a48d-a4770aa9f93b"))

		l, err := testDb.RegisterFile("tmp/file1.c4gh", "dummy")
		assert.Equal(t, "074803cc-718e-4dc4-a48d-a4770aa9f93b", l)

		return err
	})

	assert.Nil(t, r, "RegisterFile returned unexpected error")
}

func GetFileID(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectQuery("SELECT DISTINCT file_id FROM sda.file_event_log where correlation_id = \\$1;").
			WithArgs("f7207667-7d2d-46f5-96af-bd11416475c0").
			WillReturnRows(sqlmock.NewRows([]string{"file_id"}).AddRow("f83976fc-7e59-4a12-ad17-0154a36e36fc"))

		i, err := testDb.GetFileID("f7207667-7d2d-46f5-96af-bd11416475c0")
		assert.Equal(t, "0f83976fc-7e59-4a12-ad17-0154a36e36fc", i)

		return err
	})

	assert.Nil(t, r, "GetFileID returned unexpected error")
}

func TestGetHeader(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		header := []byte{15, 64}
		mock.ExpectQuery("SELECT header from sda.files WHERE id = \\$1").
			WithArgs("foo").
			WillReturnRows(sqlmock.NewRows([]string{"header"}).AddRow("0f40"))

		x, err := testDb.GetHeader("foo")

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

		mock.ExpectExec("UPDATE sda.files SET header = \\$1 WHERE id = \\$2;").
			WithArgs("0f2d142830", "fb140b10-1354-4266-879e-b34ad3e64c57").
			WillReturnResult(r)

		return testDb.StoreHeader(header, "fb140b10-1354-4266-879e-b34ad3e64c57")
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
		r := sqlmock.NewResult(0, 1)
		mock.ExpectExec("SELECT sda.set_archived\\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\);").
			WithArgs("108b842a-5d8e-4189-8e8a-9f54dc22576e", "108b842a-5d8e-4189-8e8a-9f54dc22576e", file.Path, file.Size, "96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b", "SHA256").
			WillReturnResult(r)

		return testDb.SetArchived(file, "108b842a-5d8e-4189-8e8a-9f54dc22576e", "108b842a-5d8e-4189-8e8a-9f54dc22576e")
	})
	assert.Nil(t, r, "SetArchived failed unexpectedly")

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectExec("SELECT sda.set_archived\\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\);").
			WithArgs("108b842a-5d8e-4189-8e8a-9f54dc22576e", "108b842a-5d8e-4189-8e8a-9f54dc22576e", file.Path, file.Size, "96fa8f226d3801741e807533552bc4b177ac4544d834073b6a5298934d34b40b", "SHA256").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.SetArchived(file, "108b842a-5d8e-4189-8e8a-9f54dc22576e", "108b842a-5d8e-4189-8e8a-9f54dc22576e")
	})
	assert.NotNil(t, r, "SetArchived did not fail correctly")
}

func TestUpdateDatasetEvent(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		success := sqlmock.NewResult(1, 1)
		r := sqlmock.NewRows([]string{"id"})

		r.AddRow(1)

		mock.ExpectQuery("SELECT id FROM sda.datasets WHERE stable_id = \\$1;").
			WithArgs("datesetId").WillReturnRows(r)

		mock.ExpectExec("INSERT INTO "+
			"sda.file_event_log\\(file_id, event, correlation_id, user_id\\) "+
			"SELECT file_id, \\$2, \\$3, \\$4 from sda.file_dataset "+
			"WHERE dataset_id = \\$1;").
			WithArgs(1, "ready", "somecorrelationid", "mapper").
			WillReturnResult(success)

		return testDb.UpdateDatasetEvent("datesetId", "ready", "somecorrelationid", "mapper")
	})

	assert.Nil(t, r, "UpdateDatasetEvent failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		r := sqlmock.NewRows([]string{"id"})

		r.AddRow(1)

		mock.ExpectQuery("SELECT id FROM sda.datasets WHERE stable_id = \\$1;").
			WithArgs("datesetId").WillReturnRows(r)

		mock.ExpectExec("INSERT INTO " +
			"sda.file_event_log\\(file_id, event, correlation_id, user_id\\) " +
			"SELECT file_id, \\$2, \\$3, \\$4 from sda.file_dataset " +
			"WHERE dataset_id = \\$1;").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.UpdateDatasetEvent("datesetId", "ready", "somecorrelationid", "mapper")
	})

	assert.NotNil(t, r, "UpdateDatasetEvent did not fail as expected")

	log.SetOutput(os.Stdout)
}

func TestSetAccessionID(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		r := sqlmock.NewResult(10, 1)

		mock.ExpectExec("UPDATE local_ega.files SET "+
			"stable_id = \\$1 WHERE "+
			"elixir_id = \\$2 and "+
			"inbox_path = \\$3 and "+
			"decrypted_file_checksum = \\$4 and "+
			"status = 'COMPLETED';").
			WithArgs("accessionId", "nobody", "/tmp/file.c4gh", "checksum").
			WillReturnResult(r)

		return testDb.SetAccessionID("accessionId", "nobody", "/tmp/file.c4gh", "checksum")
	})

	assert.Nil(t, r, "setAccessionID failed unexpectedly")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	r = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectExec("UPDATE local_ega.files SET "+
			"stable_id = \\$1 WHERE "+
			"elixir_id = \\$2 and "+
			"inbox_path = \\$3 and "+
			"decrypted_file_checksum = \\$4 and "+
			"status = 'COMPLETED';").
			WithArgs("accessionId", "nobody", "/tmp/file.c4gh", "checksum").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.SetAccessionID("accessionId", "nobody", "/tmp/file.c4gh", "checksum")
	})

	assert.NotNil(t, r, "SetAccessionID did not fail as expected")

	log.SetOutput(os.Stdout)
}

func TestMapFilesToDataset(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		// Set up a few file sets with different accession ids.
		// for the purpose of this test we consider accession ids the same as file ids
		accessions := []string{"2f04d87f-0af6-4918-993f-437827f2ff8b",
			"a5c477ff-b06b-4211-a978-a29aa86932b0",
			"6bcd16b9-191f-46b8-92af-cef923c47206",
			"35a7e68b-3062-4c91-b426-ca542313e4ce",
			"e5b738ee-54c0-4128-854a-b4f055c907f4",
			"98cddc6b-b836-41ab-8727-9beda6f4bdf",
			"2eb6aa2c-047e-4ff1-bf70-2dbf8fa2bf11",
			"a35836f4-8db4-4ad8-a389-ae1321120898",
			"efb14df1-0f9e-4b39-a9c2-8c310b3d9935",
			"9484d348-ce14-4a6b-a2b9-f73923f95fa0"}

		diSet := map[string][]string{"dataset1": accessions[0:3],
			"dataset2": accessions[4:5],
			"dataset3": accessions[6:8],
			"dataset4": accessions[9:9]}

		success := sqlmock.NewResult(1, 1)

		for di, acs := range diSet {

			mock.ExpectExec("INSERT INTO sda.datasets \\(stable_id\\) VALUES \\(\\$1\\) " +
				"ON CONFLICT DO NOTHING;").
				WithArgs(di).
				WillReturnResult(success)
			mock.ExpectBegin()
			for _, aID := range acs {

				r := sqlmock.NewRows([]string{"id"})

				r.AddRow(aID)

				mock.ExpectQuery("SELECT id FROM sda.files WHERE stable_id = \\$1;").
					WithArgs(aID).WillReturnRows(r)

				mock.ExpectExec("INSERT INTO sda.file_dataset "+
					"\\(file_id, dataset_id\\) SELECT \\$1, id FROM sda.datasets WHERE stable_id = \\$2 "+
					"ON CONFLICT DO NOTHING;").
					WithArgs(aID, di).WillReturnResult(success)

			}
			mock.ExpectCommit()
			err := testDb.MapFilesToDataset(di, acs)

			if !assert.Nil(t, err, "MapFilesToDataset failed unexpectedly") {
				return err
			}
		}

		var buf bytes.Buffer
		log.SetOutput(&buf)

		buf.Reset()

		mock.ExpectExec("INSERT INTO sda.datasets \\(stable_id\\) VALUES \\(\\$1\\) " +
			"ON CONFLICT DO NOTHING;").
			WithArgs("dataset").
			WillReturnResult(success)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM sda.files WHERE stable_id = \\$1;").
			WithArgs("aid1").WillReturnError(fmt.Errorf("error for testing"))
		mock.ExpectRollback().WillReturnError(fmt.Errorf("error again"))

		err := testDb.MapFilesToDataset("dataset", []string{"aid1"})

		assert.NotZero(t, buf.Len(), "Expected warning missing")
		assert.NotNil(t, err, "MapFilesToDataset did not fail as expected")

		buf.Reset()

		mock.ExpectExec("INSERT INTO sda.datasets \\(stable_id\\) VALUES \\(\\$1\\) " +
			"ON CONFLICT DO NOTHING;").
			WithArgs("dataset").
			WillReturnResult(success)

		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM sda.files WHERE stable_id = \\$1;").
			WithArgs("aid1").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("9484d348-ce14-4a6b-a2b9-f73923f95fa0"))

		mock.ExpectExec("INSERT INTO sda.file_dataset "+
			"\\(file_id, dataset_id\\) SELECT \\$1, id FROM sda.datasets WHERE stable_id = \\$2 "+
			"ON CONFLICT DO NOTHING;").
			WithArgs("9484d348-ce14-4a6b-a2b9-f73923f95fa0", "dataset").WillReturnError(fmt.Errorf("error for testing"))

		mock.ExpectRollback().WillReturnError(fmt.Errorf("error again"))

		err = testDb.MapFilesToDataset("dataset", []string{"aid1"})

		assert.NotZero(t, buf.Len(), "Expected warning missing")
		assert.NotNil(t, err, "MapFilesToDataset did not fail as expected")

		log.SetOutput(os.Stdout)

		return nil
	})

	assert.Nil(t, r, "Tests for MapFilesToDataset failed unexpectedly")
}

func TestUpdateFileStatus(t *testing.T) {
	err := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		r := sqlmock.NewResult(0, 1)
		mock.ExpectExec("INSERT INTO sda.file_event_log\\(file_id, event, correlation_id, user_id, message\\) VALUES\\(\\$1, \\$2, \\$3, \\$4, \\$5\\);").
			WithArgs("7559caae-a17c-40ae-bdb9-3a7d33408c49", "error", "f83976fc-7e59-4a12-ad17-0154a36e36fc", "dummy", "{\"json\":\"data\"}").
			WillReturnResult(r)

		return testDb.UpdateFileStatus("7559caae-a17c-40ae-bdb9-3a7d33408c49", "error", "f83976fc-7e59-4a12-ad17-0154a36e36fc", "dummy", "{\"json\":\"data\"}")
	})
	assert.Nil(t, err, "UpdateFileStatus failed unexpectedly")

	err = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectExec("INSERT INTO sda.file_event_log\\(file_id, event, correlation_id, user_id, message\\) VALUES\\(\\$1, \\$2, \\$3, \\$4, \\$5\\);").
			WithArgs("bad-uuid", "error", "f83976fc-7e59-4a12-ad17-0154a36e36fc", "dummy", "").
			WillReturnError(fmt.Errorf("error for testing"))

		return testDb.UpdateFileStatus("bad-uuid", "error", "f83976fc-7e59-4a12-ad17-0154a36e36fc", "dummy", "")
	})

	assert.NotNil(t, err, "UpdateFileStatus did not fail as expected")
}

func TestGetFileStatus(t *testing.T) {
	err := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectQuery("SELECT event from sda.file_event_log WHERE correlation_id = \\$1 ORDER BY id DESC LIMIT 1;").
			WithArgs("7559caae-a17c-40ae-bdb9-3a7d33408c49").
			WillReturnRows(sqlmock.NewRows([]string{"event"}).AddRow("error"))

		_, err := testDb.GetFileStatus("7559caae-a17c-40ae-bdb9-3a7d33408c49")

		return err
	})
	assert.Nil(t, err, "GetFileStatus failed unexpectedly")

	err = sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {
		mock.ExpectQuery("SELECT event from sda.file_event_log WHERE correlation_id = \\$1 ORDER BY id DESC LIMIT 1;").
			WithArgs("bad-uuid").
			WillReturnError(fmt.Errorf("error for testing"))

		_, err := testDb.GetFileStatus("bad-uuid")

		return err
	})

	assert.NotNil(t, err, "GetFileStatus did not fail as expected")
}

func TestGetInboxPath(t *testing.T) {
	err := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectQuery("SELECT submission_file_path from sda.files WHERE stable_id = \\$1;").
			WithArgs("EGAF00000000001").
			WillReturnRows(sqlmock.NewRows([]string{"submission_file_path"}).AddRow("test-user/file.c4gh"))

		_, err := testDb.GetInboxPath("EGAF00000000001")

		return err
	})
	assert.Nil(t, err, "GetInboxPath failed unexpectedly")
}

func TestClose(t *testing.T) {
	r := sqlTesterHelper(t, func(mock sqlmock.Sqlmock, testDb *SQLdb) error {

		mock.ExpectClose()
		testDb.Close()

		return nil
	})

	assert.Nil(t, r, "Close failed unexpectedly")
}
