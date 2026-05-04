package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/stretchr/testify/assert"
)

// TestRegisterFile tests that RegisterFile() behaves as intended
func (ts *DatabaseTests) TestRegisterFile() {
	for _, step := range []struct {
		id       string
		name     string
		location string
		filePath string
		userName string
	}{
		{
			id:       "",
			name:     "inbox",
			location: "/inbox",
			filePath: "/first/file.c4gh",
			userName: "first.user@example.org",
		},
		{
			id:       "8d2bdbc0-3b1e-443d-96b4-69d5066b8142",
			name:     "ingest",
			location: "/inbox",
			filePath: "/second/file.c4gh",
			userName: "second.user@example.org",
		},
	} {
		ts.T().Run(step.name, func(t *testing.T) {
			fileID, err := ts.db.RegisterFile(context.TODO(), &step.id, "/inbox", step.filePath, step.userName)
			assert.NoError(ts.T(), err, "RegisterFile encountered an unexpected error: ", err)
			assert.NoError(ts.T(), uuid.Validate(fileID), "RegisterFile did not return a UUID")
		})
	}
}

func (ts *DatabaseTests) TestRegisterFile_Twice() {
	fileID1, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file3.c4gh", "testuser")
	ts.NoError(err)
	fileID2, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file3.c4gh", "testuser")
	ts.NoError(err)
	ts.Equal(fileID1, fileID2)
}

func (ts *DatabaseTests) TestRegisterFileWithID() {
	insertedFileID := uuid.New().String()
	fileID, err := ts.db.RegisterFile(context.TODO(), &insertedFileID, "/inbox", "/testuser/file3.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "testuser", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file status")

	fID, err := ts.db.GetFileIDByUserPathAndStatus(context.TODO(), "testuser", "/testuser/file3.c4gh", "uploaded")
	assert.NoError(ts.T(), err, "GetFileId failed")
	assert.Equal(ts.T(), insertedFileID, fileID)
	assert.Equal(ts.T(), fileID, fID)
}

func (ts *DatabaseTests) TestUpdateFileEventLog() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file4.c4gh", "testuser")
	assert.Nil(ts.T(), err, "failed to register file in database")

	// Attempt to mark a file that doesn't exist as uploaded
	err = ts.db.UpdateFileEventLog(context.TODO(), "00000000-0000-0000-0000-000000000000", "uploaded", "testuser", "{}", "{}")
	assert.NotNil(ts.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "testuser", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to set file as uploaded in database")

	exists := false
	// check that there is an "uploaded" file event connected to the file
	err = ts.verificationDB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='uploaded')", fileID).Scan(&exists)
	assert.NoError(ts.T(), err, "Failed to check if uploaded file event exists")
	assert.True(ts.T(), exists, "UpdateFileEventLog() did not insert a row into sda.file_event_log with id: "+fileID)
}

func (ts *DatabaseTests) TestStoreHeader() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestStoreHeader.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.db.StoreHeader(context.TODO(), []byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(ts.T(), err, "failed to store file header")

	// store header for non existing entry
	err = ts.db.StoreHeader(context.TODO(), []byte{15, 45, 20, 40, 48}, "00000000-0000-0000-0000-000000000000")
	assert.EqualError(ts.T(), err, "something went wrong with the query zero rows were changed")
}

func (ts *DatabaseTests) TestRotateHeaderKey() {
	// Register a new key and a new file
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file1.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")
	err = ts.db.AddKeyHash(context.TODO(), "someKeyHash", "this is a test key")
	assert.NoError(ts.T(), err, "failed to register key in database")
	err = ts.db.StoreHeader(context.TODO(), []byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(ts.T(), err, "failed to store file header")

	// test happy path
	newKeyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507`
	err = ts.db.AddKeyHash(context.TODO(), newKeyHex, "new key")
	assert.NoError(ts.T(), err, "failed to register key in database")
	newHHeader := []byte{1, 2, 3}

	err = ts.db.RotateHeaderKey(context.TODO(), newHHeader, newKeyHex, fileID)
	assert.NoError(ts.T(), err)

	// Verify that the key+header were updated
	var dbHeaderString, dbKeyHash string
	err = ts.verificationDB.QueryRow("SELECT header, key_hash FROM sda.files WHERE id=$1", fileID).Scan(&dbHeaderString, &dbKeyHash)
	assert.NoError(ts.T(), err)
	dbHeader, err := hex.DecodeString(dbHeaderString)
	assert.NoError(ts.T(), err, "hex decoding of rotated header failed")
	assert.Equal(ts.T(), newHHeader, dbHeader)
	assert.Equal(ts.T(), newKeyHex, dbKeyHash)

	// case of non registered keyhash
	err = ts.db.RotateHeaderKey(context.TODO(), []byte{2, 4, 6, 8}, "unknownKeyHash", fileID)
	assert.ErrorContains(ts.T(), err, "violates foreign key constraint")
	// check that no column was updated
	err = ts.verificationDB.QueryRow("SELECT header, key_hash FROM sda.files WHERE id=$1", fileID).Scan(&dbHeaderString, &dbKeyHash)
	assert.NoError(ts.T(), err)
	dbHeader, err = hex.DecodeString(dbHeaderString)
	assert.NoError(ts.T(), err, "hex decoding of rotated header failed")
	assert.Equal(ts.T(), newHHeader, dbHeader)
	assert.Equal(ts.T(), newKeyHex, dbKeyHash)

	// case of non existing entry
	err = ts.db.RotateHeaderKey(context.TODO(), []byte{15, 45, 20, 40, 48}, "keyHex", "00000000-0000-0000-0000-000000000000")
	assert.EqualError(ts.T(), err, "something went wrong with the query zero rows were changed")
}

func (ts *DatabaseTests) TestSetArchived() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetArchived.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/Test/SetArchived.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     -1,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	for _, step := range []struct {
		name          string
		expectedError string
		fileID        string
		fileInfo      *database.FileInfo
		location      string
	}{
		{
			name:          "all_ok",
			location:      "/archive",
			fileID:        fileID,
			fileInfo:      fileInfo,
			expectedError: "",
		},
		{
			name:          "same_file_different_id",
			location:      "/archive",
			fileID:        "0d57f640-4669-44cc-91bf-5b2fcc605ddc",
			fileInfo:      fileInfo,
			expectedError: "violates foreign key constraint",
		},
	} {
		ts.T().Run(step.name, func(t *testing.T) {
			err := ts.db.SetArchived(context.TODO(), step.location, step.fileInfo, step.fileID)
			if step.expectedError != "" {
				assert.ErrorContains(ts.T(), err, step.expectedError)
			} else {
				assert.NoError(ts.T(), err, "SetArchived encountered an unexpected error: ", err)
			}
		})
	}
}

func (ts *DatabaseTests) TestGetFileStatus() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetFileStatus.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "downloaded", "testuser", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to set file as downloaded in database")

	status, err := ts.db.GetFileStatus(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "failed to get file status")
	assert.Equal(ts.T(), "downloaded", status)
}

func (ts *DatabaseTests) TestGetHeader() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetHeader.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.db.StoreHeader(context.TODO(), []byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(ts.T(), err, "failed to store file header")

	header, err := ts.db.GetHeader(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "failed to get file header")
	assert.Equal(ts.T(), []byte{15, 45, 20, 40, 48}, header)
}

func (ts *DatabaseTests) TestBackupHeader() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestBackupHeader.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	testKeyHash := "test-key-hash-123"
	_, err = ts.verificationDB.Exec("INSERT INTO sda.encryption_keys (key_hash) VALUES ($1) ON CONFLICT DO NOTHING", testKeyHash)
	assert.NoError(ts.T(), err, "failed to setup test encryption key")

	testHeader := []byte{1, 2, 3, 4, 5}
	err = ts.db.BackupHeader(context.TODO(), fileID, testHeader, testKeyHash)
	assert.NoError(ts.T(), err, "failed to backup header")

	var storedHeaderHex string
	var storedKeyHash string

	query := "SELECT header, key_hash FROM sda.file_headers_backup WHERE file_id = $1"
	err = ts.verificationDB.QueryRow(query, fileID).Scan(&storedHeaderHex, &storedKeyHash)

	assert.NoError(ts.T(), err, "failed to find backup record in database")
	assert.Equal(ts.T(), hex.EncodeToString(testHeader), storedHeaderHex)
	assert.Equal(ts.T(), testKeyHash, storedKeyHash)
}

func (ts *DatabaseTests) TestSetVerified() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetVerified.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/testuser/TestSetVerified.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     948,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)

	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)
}

func (ts *DatabaseTests) TestGetArchived() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetArchived.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/tmp/TestGetArchived.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)

	archiveData, err := ts.db.GetArchived(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(ts.T(), int64(1000), archiveData.FileSize)
	assert.Equal(ts.T(), "/tmp/TestGetArchived.c4gh", archiveData.FilePath)
	assert.Equal(ts.T(), "/archive", archiveData.Location)
}

func (ts *DatabaseTests) TestSetAccessionID() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/tmp/TestSetAccessionID.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)
	accessionID := "TEST:000-1234-4567"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)
}

func (ts *DatabaseTests) TestCheckAccessionIDExists() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestCheckAccessionIDExists.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/tmp/TestCheckAccessionIDExists.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)
	accessionID := "TEST:111-1234-4567"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

	exists, err := ts.db.CheckAccessionIDExists(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(ts.T(), "same", exists)

	duplicate, err := ts.db.CheckAccessionIDExists(context.TODO(), accessionID, uuid.New().String())
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(ts.T(), "duplicate", duplicate)
}

func (ts *DatabaseTests) TestGetAccessionID() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/tmp/TestSetAccessionID.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)
	accessionID := "TEST:000-1234-4567"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

	res, err := ts.db.GetAccessionID(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting accessionID of file", err)
	assert.Equal(ts.T(), accessionID, res, "retrieved accessionID is wrong")
}

func (ts *DatabaseTests) TestGetAccessionID_wrongFileID() { // register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1000,
		Path:              "/tmp/TestSetAccessionID.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)
	accessionID := "TEST:000-1234-4567"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

	// check for bad format
	_, err = ts.db.GetAccessionID(context.TODO(), "someFileID")
	assert.ErrorContains(ts.T(), err, "invalid input syntax for type uuid")

	// check for non-existent fileID
	_, err = ts.db.GetAccessionID(context.TODO(), uuid.New().String())
	assert.ErrorContains(ts.T(), err, "no rows in result set")
}

func (ts *DatabaseTests) TestGetFileInfo() { // register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetFileInfo.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(ts.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(ts.T(), err)

	fileInfo := &database.FileInfo{
		Size:              2000,
		Path:              "/tmp/TestGetFileInfo.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     1987,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as Archived")
	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "got (%v) when marking file as verified", err)

	info, err := ts.db.GetFileInfo(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(ts.T(), int64(2000), info.Size)
	assert.Equal(ts.T(), "/tmp/TestGetFileInfo.c4gh", info.Path)
	assert.Equal(ts.T(), "11c94bc7fb13afeb2b3fb16c1dbe9206dc09560f1b31420f2d46210ca4ded0a8", info.ArchivedChecksum)
	assert.Equal(ts.T(), "a671218c2418aa51adf97e33c5c91a720289ba3c9fd0d36f6f4bf9610730749f", info.DecryptedChecksum)
}

func (ts *DatabaseTests) TestMapFilesToDataset() {
	fileIDs := []string{}
	for i := 1; i < 12; i++ {
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", fmt.Sprintf("/testuser/TestMapFilesToDataset-%d.c4gh", i), "testuser")
		assert.NoError(ts.T(), err, "failed to register file in database")

		err = ts.db.SetAccessionID(context.TODO(), fmt.Sprintf("acession-%d", i), fileID)
		assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

		fileIDs = append(fileIDs, fileID)
	}

	diSet := map[string][]string{
		"dataset1": fileIDs[0:3],
		"dataset2": fileIDs[3:5],
		"dataset3": fileIDs[5:8],
		"dataset4": fileIDs[8:9],
	}

	for di, fIDs := range diSet {
		for _, fileID := range fIDs {
			err := ts.db.MapFileToDataset(context.TODO(), di, fileID)
			assert.NoError(ts.T(), err, "failed to map file to dataset")
		}
	}

	// Append files to an existing dataset
	for _, fileID := range fileIDs[9:11] {
		err := ts.db.MapFileToDataset(context.TODO(), "dataset1", fileID)
		assert.NoError(ts.T(), err, "failed to append file to dataset")
	}

	var dsMembers int
	const q1 = "SELECT count(file_id) from sda.file_dataset WHERE dataset_id = (SELECT id from sda.datasets WHERE stable_id = $1);"
	if err := ts.verificationDB.QueryRow(q1, "dataset1").Scan(&dsMembers); err != nil {
		ts.FailNow("failed to get dataset members from database")
	}
	assert.Equal(ts.T(), 5, dsMembers)
}

func (ts *DatabaseTests) TestGetInboxPath() {
	accessions := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
		assert.NoError(ts.T(), err, "failed to register file in database")

		err = ts.db.SetAccessionID(context.TODO(), fmt.Sprintf("acession-00%d", i), fileID)
		assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fmt.Sprintf("acession-00%d", i))
	}

	for _, acc := range accessions {
		path, err := ts.db.GetInboxPath(context.TODO(), acc)
		assert.NoError(ts.T(), err, "getInboxPath failed")
		assert.Contains(ts.T(), path, "/testuser/TestGetInboxPath")
	}
}

func (ts *DatabaseTests) TestUpdateDatasetEvent() {
	fileIDs := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
		assert.NoError(ts.T(), err, "failed to register file in database")

		err = ts.db.SetAccessionID(context.TODO(), fmt.Sprintf("acession-00%d", i), fileID)
		assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

		fileIDs = append(fileIDs, fileID)
	}

	diSet := map[string][]string{"DATASET:TEST-0001": fileIDs}

	for di, fIDs := range diSet {
		for _, fileID := range fIDs {
			assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), di, fileID), "failed to map file to dataset")
		}
	}

	dID := "DATASET:TEST-0001"
	ts.NoError(ts.db.UpdateDatasetEvent(context.TODO(), dID, "registered", "{\"type\": \"mapping\"}"))
	ts.NoError(ts.db.UpdateDatasetEvent(context.TODO(), dID, "released", "{\"type\": \"release\"}"))
	ts.NoError(ts.db.UpdateDatasetEvent(context.TODO(), dID, "deprecated", "{\"type\": \"deprecate\"}"))
}

func (ts *DatabaseTests) TestGetHeaderForAccessionID() { // register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetHeaderForAccessionID.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.db.StoreHeader(context.TODO(), []byte("HEADER"), fileID)
	assert.NoError(ts.T(), err, "failed to store file header")

	accessionID := "TEST:010-1234-4567"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when setting stable ID: %s, %s", err, accessionID, fileID)

	header, err := ts.db.GetHeaderByAccessionID(context.TODO(), "TEST:010-1234-4567")
	assert.NoError(ts.T(), err, "failed to get header for stable ID: %v", err)
	assert.Equal(ts.T(), header, []byte("HEADER"), "did not get expected header")
}

func (ts *DatabaseTests) TestGetSyncData() { // register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetGetSyncData.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1234,
		Path:              "/tmp/TestGetGetSyncData.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "failed to mark file as Archived")

	err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
	assert.NoError(ts.T(), err, "failed to mark file as Verified")

	accessionID := "TEST:000-1111-2222"
	err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
	assert.NoError(ts.T(), err, "got (%v) when setting stable ID: %s, %s", err, accessionID, fileID)

	fileData, err := ts.db.GetSyncData(context.TODO(), "TEST:000-1111-2222")
	assert.NoError(ts.T(), err, "failed to get sync data for file")
	assert.Equal(ts.T(), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", fileData.Checksum, "did not get expected file checksum")
	assert.Equal(ts.T(), "/testuser/TestGetGetSyncData.c4gh", fileData.FilePath, "did not get expected file path")
	assert.Equal(ts.T(), "testuser", fileData.User, "did not get expected user")
}

func (ts *DatabaseTests) TestCheckIfDatasetExists() {
	accessions := []string{}
	for i := 0; i <= 3; i++ {
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", fmt.Sprintf("/testuser/TestCheckIfDatasetExists-%d.c4gh", i), "testuser")
		assert.NoError(ts.T(), err, "failed to register file in database")

		err = ts.db.SetAccessionID(context.TODO(), fmt.Sprintf("accession-%d", i), fileID)
		assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fileID)
	}

	diSet := map[string][]string{
		"dataset": accessions[0:3],
	}

	for di, fIDs := range diSet {
		for _, fileIDs := range fIDs {
			assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), di, fileIDs), "failed to map file to dataset")
		}
	}

	ok, err := ts.db.CheckIfDatasetExists(context.TODO(), "dataset")
	assert.NoError(ts.T(), err, "check if dataset exists failed")
	assert.Equal(ts.T(), ok, true)

	ok, err = ts.db.CheckIfDatasetExists(context.TODO(), "missing dataset")
	assert.NoError(ts.T(), err, "check if dataset exists failed")
	assert.Equal(ts.T(), ok, false)
}

func (ts *DatabaseTests) TestGetArchivePath() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetArchivePath-001.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileInfo := &database.FileInfo{
		Size:              1234,
		Path:              uuid.New().String(),
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
	assert.NoError(ts.T(), err, "failed to mark file as Archived")

	err = ts.db.SetAccessionID(context.TODO(), "acession-0001", fileID)
	assert.NoError(ts.T(), err, "got (%v) when getting file archive information", err)

	path, location, err := ts.db.GetArchivePathAndLocation(context.TODO(), "acession-0001")
	assert.NoError(ts.T(), err, "getArchivePathAndLocation failed")
	assert.Equal(ts.T(), fileInfo.Path, path)
	assert.Equal(ts.T(), "/archive", location)
}

func (ts *DatabaseTests) TestGetUserFiles() {
	testCases := 5
	testUser := "GetFilesUser"
	sub := "submission_a"

	for i := range testCases {
		if i == 2 {
			sub = "submission_b"
		}

		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", fmt.Sprintf("%v/%s/TestGetUserFiles-00%d.c4gh", testUser, sub, i), testUser)
		assert.NoError(ts.T(), err, "failed to register file in database")
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", testUser, "{}", "{}")
		assert.NoError(ts.T(), err, "failed to update satus of file in database")
		err = ts.db.SetAccessionID(context.TODO(), fmt.Sprintf("accessionID-00%d", i), fileID)
		assert.NoError(ts.T(), err, "failed to update satus of file in database")
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "ready", testUser, "{}", "{}")
		assert.NoError(ts.T(), err, "failed to update satus of file in database")
	}
	filelist, err := ts.db.GetUserFiles(context.TODO(), "unknownuser", "", true)
	assert.NoError(ts.T(), err, "failed to get (empty) file list of unknown user")
	assert.Empty(ts.T(), filelist, "file list of unknown user is not empty")

	filelist, err = ts.db.GetUserFiles(context.TODO(), testUser, "", true)
	assert.NoError(ts.T(), err, "failed to get file list")
	assert.Equal(ts.T(), testCases, len(filelist), "file list is of incorrect length")

	for _, fileInfo := range filelist {
		assert.Equal(ts.T(), "ready", fileInfo.Status, "incorrect file status")
		assert.Contains(ts.T(), fileInfo.AccessionID, "accessionID-00", "incorrect file accession ID")
	}

	filteredFilelist, err := ts.db.GetUserFiles(context.TODO(), testUser, fmt.Sprintf("%s/submission_b", testUser), true)
	assert.NoError(ts.T(), err, "failed to get file list")
	assert.Equal(ts.T(), 3, len(filteredFilelist), "file list is of incorrect length")
}

func (ts *DatabaseTests) TestGetCorrID_sameFilePath() {
	filePath := "/testuser/file10.c4gh"
	user := "testuser"

	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
	if err != nil {
		ts.FailNow("failed to register file in database")
	}
	if err := ts.db.UpdateFileEventLog(context.TODO(), fileID, "archived", user, "{}", "{}"); err != nil {
		ts.FailNow("failed to update satus of file in database")
	}

	fileInfo := &database.FileInfo{
		Size:              1234,
		Path:              fileID,
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	if err := ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID); err != nil {
		ts.FailNow("failed to mark file as archived")
	}
	if err := ts.db.UpdateFileEventLog(context.TODO(), fileID, "archived", user, "{}", "{}"); err != nil {
		ts.FailNow("failed to update satus of file in database")
	}
	if err := ts.db.SetAccessionID(context.TODO(), "accessionID", fileID); err != nil {
		ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "accessionID", fileID)
	}

	fileID2, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
	assert.NoError(ts.T(), err, "failed to register file in database")
	if err := ts.db.UpdateFileEventLog(context.TODO(), fileID2, "uploaded", user, "{}", "{}"); err != nil {
		ts.FailNow("failed to update satus of file in database")
	}
	assert.NotEqual(ts.T(), fileID, fileID2)
}
func (ts *DatabaseTests) TestListActiveUsers() {
	testCases := 5
	testUsers := []string{"User-A", "User-B", "User-C", "User-D"}

	accessionToFileID := make(map[string]string)
	for _, user := range testUsers {
		for i := 0; i < testCases; i++ {
			filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i)
			fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
			if err != nil {
				ts.FailNow("Failed to register file")
			}
			err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", user, "{}", "{}")
			if err != nil {
				ts.FailNow("Failed to update file event log")
			}

			fileInfo := &database.FileInfo{
				Size:              1234,
				Path:              filePath,
				ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
				DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
				DecryptedSize:     999,
				UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			}

			err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
			if err != nil {
				ts.FailNow("failed to mark file as Archived")
			}

			err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
			if err != nil {
				ts.FailNow("failed to mark file as Verified")
			}

			accessionID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
			if err != nil {
				ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
			}
			accessionToFileID[accessionID] = fileID
		}
	}

	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-A_00"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-A_01"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-A_02"]))

	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-C_00"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-C_01"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-C_02"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-C_03"]))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "test-dataset-01", accessionToFileID["accession_User-C_04"]))

	userList, err := ts.db.ListActiveUsers(context.TODO())
	assert.NoError(ts.T(), err, "failed to list users from DB")
	assert.Equal(ts.T(), 3, len(userList))
}

func (ts *DatabaseTests) TestGetDatasetStatus() {
	testCases := 5

	dID := "test-get-dataset-01"
	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", "User-Q", i)
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, "User-Q")
		if err != nil {
			ts.FailNow("Failed to register file")
		}
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			ts.FailNow("Failed to update file event log")
		}

		fileInfo := &database.FileInfo{
			Size:              1234,
			Path:              filePath,
			ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedSize:     999,
			UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		}

		err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Archived")
		}

		err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Verified")
		}

		accessionID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
		if err != nil {
			ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
		}

		if err := ts.db.MapFileToDataset(context.TODO(), dID, fileID); err != nil {
			ts.FailNow("failed to map files to dataset")
		}
	}

	err := ts.db.UpdateDatasetEvent(context.TODO(), dID, "registered", "{\"type\": \"mapping\"}")
	assert.NoError(ts.T(), err, "got (%v) when updating dataset event", err)
	status, err := ts.db.GetDatasetStatus(context.TODO(), dID)
	assert.NoError(ts.T(), err, "got (%v) when no error weas expected")
	assert.Equal(ts.T(), "registered", status)

	err = ts.db.UpdateDatasetEvent(context.TODO(), dID, "released", "{\"type\": \"mapping\"}")
	assert.NoError(ts.T(), err, "got (%v) when updating dataset event", err)
	status, err = ts.db.GetDatasetStatus(context.TODO(), dID)
	assert.NoError(ts.T(), err, "got (%v) when no error weas expected")
	assert.Equal(ts.T(), "released", status)

	err = ts.db.UpdateDatasetEvent(context.TODO(), dID, "deprecated", "{\"type\": \"mapping\"}")
	assert.NoError(ts.T(), err, "got (%v) when updating dataset event", err)
	status, err = ts.db.GetDatasetStatus(context.TODO(), dID)
	assert.NoError(ts.T(), err, "got (%v) when no error weas expected")
	assert.Equal(ts.T(), "deprecated", status)
}

func (ts *DatabaseTests) TestAddKeyHash() {
	// Test registering a new key and its description
	keyHex := `cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23`
	keyDescription := "this is a test key"
	err := ts.db.AddKeyHash(context.TODO(), keyHex, keyDescription)
	assert.NoError(ts.T(), err, "failed to register key in database")

	// Verify that the key was added
	var exists bool
	err = ts.verificationDB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.encryption_keys WHERE key_hash=$1 AND description=$2)", keyHex, keyDescription).Scan(&exists)
	assert.NoError(ts.T(), err, "failed to verify key hash existence")
	assert.True(ts.T(), exists, "key hash was not added to the database")
}

func (ts *DatabaseTests) TestListKeyHashes() {
	assert.NoError(ts.T(), ts.db.AddKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")
	assert.NoError(ts.T(), ts.db.AddKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99", "this is a another key"), "failed to register key in database")

	expectedResponse := &database.C4ghKeyHash{
		Hash:         "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23",
		Description:  "this is a test key",
		CreatedAt:    time.Now().UTC().Format(time.DateOnly),
		DeprecatedAt: "",
	}
	hashList, err := ts.db.ListKeyHashes(context.TODO())
	ct, _ := time.Parse(time.RFC3339, hashList[0].CreatedAt)
	hashList[0].CreatedAt = ct.Format(time.DateOnly)
	assert.NoError(ts.T(), err, "failed to verify key hash existence")
	assert.Equal(ts.T(), expectedResponse, hashList[0], "key hash was not added to the database")
}

func (ts *DatabaseTests) TestListKeyHashes_emptyTable() {
	hashList, err := ts.db.ListKeyHashes(context.TODO())
	assert.NoError(ts.T(), err, "failed to verify key hash existence")
	assert.Equal(ts.T(), 0, len(hashList), "key has is not empty when expected")
}

func (ts *DatabaseTests) TestDeprecateKeyHashes() {
	assert.NoError(ts.T(), ts.db.AddKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc32", "this is a test key"), "failed to register key in database")
	assert.NoError(ts.T(), ts.db.DeprecateKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc32"), "failure when deprecating keyhash")
}

func (ts *DatabaseTests) TestDeprecateKeyHashes_wrongHash() {
	assert.NoError(ts.T(), ts.db.AddKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc11", "this is a another key"), "failed to register key in database")
	assert.EqualError(ts.T(), ts.db.DeprecateKeyHash(context.TODO(), "wr0n6h4sh"), "key hash not found or already deprecated", "failure when deprecating non existing keyhash")
}

func (ts *DatabaseTests) TestDeprecateKeyHashes_alreadyDeprecated() {
	assert.NoError(ts.T(), ts.db.AddKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54", "this is a test key"), "failed to register key in database")
	assert.NoError(ts.T(), ts.db.DeprecateKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54"), "failure when deprecating keyhash")
	// we should not be able to change the deprecation date
	assert.EqualError(ts.T(), ts.db.DeprecateKeyHash(context.TODO(), "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54"), "key hash not found or already deprecated", "failure when deprecating keyhash")
}

func (ts *DatabaseTests) TestSetKeyHash() {
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507`
	keyDescription := "this is a test key"
	err := ts.db.AddKeyHash(context.TODO(), keyHex, keyDescription)
	assert.NoError(ts.T(), err, "failed to register key in database")
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file1.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	// Test that the key hash can be set in the files table
	err = ts.db.SetKeyHash(context.TODO(), keyHex, fileID)
	assert.NoError(ts.T(), err, "Could not set key hash")

	// Verify that the key+file was added
	var exists bool
	err = ts.verificationDB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.files WHERE key_hash=$1 AND id=$2)", keyHex, fileID).Scan(&exists)
	assert.NoError(ts.T(), err, "failed to verify key hash set for file")
	assert.True(ts.T(), exists, "key hash was not set for file in the database")
}

func (ts *DatabaseTests) TestSetKeyHash_wrongHash() {
	// Add key hash and file

	keyHex := "6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc501"
	keyDescription := "this is a test hash"
	err := ts.db.AddKeyHash(context.TODO(), keyHex, keyDescription)
	assert.NoError(ts.T(), err, "failed to register key in database")
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file2.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	// Ensure failure if a non existing hash is used
	newKeyHex := "6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc502"
	err = ts.db.SetKeyHash(context.TODO(), newKeyHex, fileID)
	assert.ErrorContains(ts.T(), err, "violates foreign key constraint")
}

func (ts *DatabaseTests) TestGetKeyHash() {
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc509`
	keyDescription := "this is a test key"
	err := ts.db.AddKeyHash(context.TODO(), keyHex, keyDescription)
	assert.NoError(ts.T(), err, "failed to register key in database")
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file1.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")
	err = ts.db.SetKeyHash(context.TODO(), keyHex, fileID)
	assert.NoError(ts.T(), err, "failed to set key hash in database")

	// Test happy path
	keyHash, err := ts.db.GetKeyHash(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "Could not get key hash")
	assert.Equal(ts.T(), keyHex, keyHash)
}

func (ts *DatabaseTests) TestGetKeyHash_wrongFileID() {
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc509`
	keyDescription := "this is a test key"
	err := ts.db.AddKeyHash(context.TODO(), keyHex, keyDescription)
	assert.NoError(ts.T(), err, "failed to register key in database")
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/file1.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")
	err = ts.db.SetKeyHash(context.TODO(), keyHex, fileID)
	assert.NoError(ts.T(), err, "failed to set key hash in database")

	// Test that using an unknown fileID produces an error
	_, err = ts.db.GetKeyHash(context.TODO(), "097e1dc9-6b42-42bf-966d-dece6fefda09")
	assert.ErrorContains(ts.T(), err, "no rows in result set")
}

func (ts *DatabaseTests) TestListDatasets() {
	testCases := 10

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", "User-Q", i)
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, "User-Q")
		if err != nil {
			ts.FailNow("Failed to register file")
		}
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			ts.FailNow("Failed to update file event log")
		}

		fileInfo := &database.FileInfo{
			Size:              1234,
			Path:              filePath,
			ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedSize:     999,
			UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		}

		err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Archived")
		}

		err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Verified")
		}

		accessionID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
		if err != nil {
			ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
		}
		dID := "test-get-dataset-01"
		switch {
		case i < 3:
		case i <= 5:
			dID = "test-get-dataset-02"
		case i <= 8:
			dID = "test-get-dataset-03"
		default:
			continue
		}
		if err := ts.db.MapFileToDataset(context.TODO(), dID, fileID); err != nil {
			ts.FailNow("failed to map files to dataset")
		}
	}

	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-01", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-01", "released", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-02", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-03", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-03", "released", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-get-dataset-03", "deprecated", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	datasets, err := ts.db.ListDatasets(context.TODO())
	assert.NoError(ts.T(), err, "got (%v) when listing datasets", err)
	assert.Equal(ts.T(), "test-get-dataset-01", datasets[0].DatasetID)
	assert.Equal(ts.T(), "registered", datasets[1].Status)
}

func (ts *DatabaseTests) TestListUserDatasets() {
	user := "User-Q"
	for i := 0; i < 6; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i)
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
		if err != nil {
			ts.FailNow("Failed to register file")
		}
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", user, "{}", "{}")
		if err != nil {
			ts.FailNow("Failed to update file event log")
		}

		fileInfo := &database.FileInfo{
			Size:              1234,
			Path:              filePath,
			ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedSize:     999,
			UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		}

		err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Archived")
		}

		err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Verified")
		}

		accessionID := fmt.Sprintf("accession_%s_0%d", user, i)
		err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
		if err != nil {
			ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
		}

		if i >= 3 {
			if err := ts.db.MapFileToDataset(context.TODO(), "test-user-dataset-02", fileID); err != nil {
				ts.FailNow("failed to map files to dataset")
			}

			continue
		}
		if err := ts.db.MapFileToDataset(context.TODO(), "test-user-dataset-01", fileID); err != nil {
			ts.FailNow("failed to map files to dataset")
		}
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-user-dataset-01", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-user-dataset-01", "released", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-user-dataset-02", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "filePath", "user")
	if err != nil {
		ts.FailNow("Failed to register file")
	}

	err = ts.db.SetAccessionID(context.TODO(), "accessionID", fileID)
	if err != nil {
		ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "accessionID", fileID)
	}

	if err := ts.db.MapFileToDataset(context.TODO(), "test-wrong-user-dataset", fileID); err != nil {
		ts.FailNow("failed to map files to dataset")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-wrong-user-dataset", "registered", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-wrong-user-dataset", "released", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}
	if err := ts.db.UpdateDatasetEvent(context.TODO(), "test-wrong-user-dataset", "deprecated", "{\"type\": \"mapping\"}"); err != nil {
		ts.FailNow("failed to update dataset event")
	}

	datasets, err := ts.db.ListUserDatasets(context.TODO(), user)
	assert.NoError(ts.T(), err, "got (%v) when listing datasets for a user", err)
	assert.Equal(ts.T(), 2, len(datasets))
	assert.Equal(ts.T(), "test-user-dataset-01", datasets[0].DatasetID)
}

func (ts *DatabaseTests) TestUpdateUserInfo() { // Insert a userID
	var groups []string
	userID, name, email := "12334556testuser@lifescience.ru", "Test User", "test.user@example.org"
	err := ts.db.UpdateUserInfo(context.TODO(), userID, name, email, groups)
	assert.NoError(ts.T(), err, "could not insert user info: %v", err)
	// Verify that the userID is connected to the details
	var numRows int
	err = ts.verificationDB.QueryRow("SELECT COUNT(*) FROM sda.userinfo WHERE id=$1", userID).Scan(&numRows)
	assert.NoError(ts.T(), err, "could select user info: %v", err)
	assert.Equal(ts.T(), 1, numRows, "there should be exactly 1 row about %v in userinfo table", userID)
	var name2 string
	err = ts.verificationDB.QueryRow("SELECT name FROM sda.userinfo WHERE id=$1", userID).Scan(&name2)
	assert.NoError(ts.T(), err, "could not select user info: %v", err)
	assert.Equal(ts.T(), name, name2, "user info table did not update correctly")
}

func (ts *DatabaseTests) TestUpdateUserInfo_newInfo() { // Insert a user
	var groups []string
	userID, name, email := "12334556testuser@lifescience.ru", "Test User", "test.user@example.org"
	err := ts.db.UpdateUserInfo(context.TODO(), userID, name, email, groups)
	assert.NoError(ts.T(), err, "could not insert user info: %v", err)
	var exists bool
	err = ts.verificationDB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.userinfo WHERE id=$1)", userID).Scan(&exists)
	assert.NoError(ts.T(), err, "failed to verify user info existence")
	assert.True(ts.T(), exists, "user info was not added to the database")

	// Insert new information about the user and verify that there is still only 1 row,
	// and that this row is updated
	var numRows int
	err = ts.verificationDB.QueryRow("SELECT COUNT(*) FROM sda.userinfo WHERE id=$1", userID).Scan(&numRows)
	assert.NoError(ts.T(), err, "could not verify db count", err)
	assert.Equal(ts.T(), 1, numRows, "there should be exactly one row in userinfo")
	var dbgroups []string
	groups = append(groups, "appleGroup", "bananaGroup")
	name = "newName"
	err = ts.db.UpdateUserInfo(context.TODO(), userID, name, email, groups)
	assert.NoError(ts.T(), err, "could not insert updated user info: %v", err)
	err = ts.verificationDB.QueryRow("SELECT groups FROM sda.userinfo WHERE id=$1", userID).Scan(pq.Array(&dbgroups))
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), groups, dbgroups)
}

func (ts *DatabaseTests) TestGetReVerificationData() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		ts.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	fileInfo := &database.FileInfo{
		Size:              2000,
		Path:              "/archive/TestGetReVerificationData.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	if err := ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID); err != nil {
		ts.FailNow("failed to archive file")
	}
	if err := ts.db.SetVerified(context.TODO(), fileInfo, fileID); err != nil {
		ts.FailNow("failed to mark file as verified")
	}
	accession := "acession-001"
	if err := ts.db.SetAccessionID(context.TODO(), accession, fileID); err != nil {
		ts.FailNow("failed to set accession id for file")
	}

	data, err := ts.db.GetReVerificationData(context.TODO(), accession)
	assert.NoError(ts.T(), err, "failed to get verification data")
	assert.Equal(ts.T(), "/archive/TestGetReVerificationData.c4gh", data.ArchiveFilePath)
}

func (ts *DatabaseTests) TestGetReVerificationDataFromFileID() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		ts.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	fileInfo := &database.FileInfo{
		Size:              2000,
		Path:              "/archive/TestGetReVerificationData.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	if err := ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID); err != nil {
		ts.FailNow("failed to archive file")
	}
	if err := ts.db.SetVerified(context.TODO(), fileInfo, fileID); err != nil {
		ts.FailNow("failed to mark file as verified")
	}

	data, err := ts.db.GetReVerificationDataFromFileID(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "failed to get verification data from fileID")
	assert.Equal(ts.T(), "/archive/TestGetReVerificationData.c4gh", data.ArchiveFilePath)
}

func (ts *DatabaseTests) TestGetReVerificationData_wrongAccessionID() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		ts.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	fileInfo := &database.FileInfo{
		Size:              2000,
		Path:              "/archive/TestGetReVerificationData.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	if err := ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID); err != nil {
		ts.FailNow("failed to archive file")
	}
	if err := ts.db.SetVerified(context.TODO(), fileInfo, fileID); err != nil {
		ts.FailNow("failed to mark file as verified")
	}
	accession := "acession-001"
	if err := ts.db.SetAccessionID(context.TODO(), accession, fileID); err != nil {
		ts.FailNow("failed to set accession id for file")
	}

	data, err := ts.db.GetReVerificationData(context.TODO(), "accession")
	assert.EqualError(ts.T(), err, "sql: no rows in result set")
	assert.Nil(ts.T(), data)
}

func (ts *DatabaseTests) TestGetDecryptedChecksum() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestGetDecryptedChecksum.c4gh", "testuser")
	if err != nil {
		ts.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		ts.FailNow("failed to generate checksum")
	}

	fileInfo := &database.FileInfo{
		Size:              2000,
		Path:              "/archive/TestGetDecryptedChecksum.c4gh",
		ArchivedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}

	if err := ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID); err != nil {
		ts.FailNow("failed to archive file")
	}
	if err := ts.db.SetVerified(context.TODO(), fileInfo, fileID); err != nil {
		ts.FailNow("failed to mark file as verified")
	}

	checksum, err := ts.db.GetDecryptedChecksum(context.TODO(), fileID)
	assert.NoError(ts.T(), err, "failed to get verification data")
	assert.Equal(ts.T(), fmt.Sprintf("%x", decSha.Sum(nil)), checksum)
}

func (ts *DatabaseTests) TestGetDatasetFiles() {
	testCases := 3
	dID := "test-get-dataset-files-01"
	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetDsatasetFiles-00%d.c4gh", "User-Q", i)
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, "User-Q")
		if err != nil {
			ts.FailNow("Failed to register file")
		}
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			ts.FailNow("Failed to update file event log")
		}

		fileInfo := &database.FileInfo{
			Size:              1234,
			Path:              filePath,
			ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedSize:     999,
			UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		}

		err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Archived")
		}

		err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Verified")
		}

		accessionID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
		if err != nil {
			ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
		}
		assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), dID, fileID))
	}

	files, err := ts.db.GetDatasetFiles(context.TODO(), dID)
	assert.NoError(ts.T(), err, "failed to get files for a dataset")
	assert.Equal(ts.T(), 3, len(files))

	assert.ElementsMatch(ts.T(), []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}, files)
}

func (ts *DatabaseTests) TestGetDatasetFileIDs() {
	testCases := 3
	var createdFileIDs []string
	dID := "test-get-dataset-fileids-01"

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetDatasetFileIDs-00%d.c4gh", "User-Q", i)
		fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, "User-Q")
		if err != nil {
			ts.FailNow("Failed to register file")
		}
		createdFileIDs = append(createdFileIDs, fileID)
		err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			ts.FailNow("Failed to update file event log")
		}

		fileInfo := &database.FileInfo{
			Size:              1234,
			Path:              filePath,
			ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
			DecryptedSize:     999,
			UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		}

		err = ts.db.SetArchived(context.TODO(), "/archive", fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Archived")
		}

		err = ts.db.SetVerified(context.TODO(), fileInfo, fileID)
		if err != nil {
			ts.FailNow("failed to mark file as Verified")
		}

		accessionID := fmt.Sprintf("accession_ids_%s_0%d", "User-Q", i)
		err = ts.db.SetAccessionID(context.TODO(), accessionID, fileID)
		if err != nil {
			ts.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), accessionID, fileID)
		}
		assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), dID, fileID))
	}

	files, err := ts.db.GetDatasetFileIDs(context.TODO(), dID)
	assert.NoError(ts.T(), err, "failed to get files for a dataset")
	assert.Equal(ts.T(), 3, len(files))

	assert.ElementsMatch(ts.T(), createdFileIDs, files)
}

func (ts *DatabaseTests) TestGetSubmissionPathAndLocation() {
	user := "UserX"
	filePath := fmt.Sprintf("/%v/Deletefile1.c4gh", user)
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
	if err != nil {
		ts.FailNow("Failed to register file")
	}
	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", "User-z", "{}", "{}")
	if err != nil {
		ts.FailNow("Failed to update file event log")
	}
	path, location, err := ts.db.GetUploadedSubmissionFilePathAndLocation(context.TODO(), user, fileID)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), path, filePath)
	assert.Equal(ts.T(), "/inbox", location)

	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "archived", user, "{}", "{}")
	assert.NoError(ts.T(), err)
	_, _, err = ts.db.GetUploadedSubmissionFilePathAndLocation(context.TODO(), user, fileID)
	assert.Error(ts.T(), err)
}

func (ts *DatabaseTests) TestGetFileIDByUserPathAndStatus() {
	user := "UserX"
	filePath := fmt.Sprintf("/%v/Deletefile1.c4gh", user)
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
	if err != nil {
		ts.FailNow("Failed to register file")
	}
	// sanity check - should fail
	_, err = ts.db.GetFileIDByUserPathAndStatus(context.TODO(), "wrong-user", filePath, "registered")
	assert.EqualError(ts.T(), err, "sql: no rows in result set")

	// check happy path
	fileID2, err := ts.db.GetFileIDByUserPathAndStatus(context.TODO(), user, filePath, "registered")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), fileID, fileID2)

	// update the status of the file
	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "archived", user, "{}", "{}")
	if err != nil {
		ts.FailNow("Failed to update file event log")
	}

	// check that an error and an empty fileID are returned when the most recent status is not registered
	fileID2, err = ts.db.GetFileIDByUserPathAndStatus(context.TODO(), user, filePath, "registered")
	assert.Error(ts.T(), err)
	assert.Equal(ts.T(), "", fileID2)

	// check that the function works for other statuses as well
	fileID2, err = ts.db.GetFileIDByUserPathAndStatus(context.TODO(), user, filePath, "archived")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), fileID, fileID2)
}

func (ts *DatabaseTests) TestGetFileDetailsFromUUI_Found() {
	// Register a file to get a valid UUID
	filePath := "/dummy_user.org/Dummy_folder/dummyfile.c4gh"
	user := "dummy@user.org"
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", filePath, user)
	if err != nil {
		ts.FailNow("failed to register file in database")
	}

	// Update event log to ensure correlation ID is set
	err = ts.db.UpdateFileEventLog(context.TODO(), fileID, "uploaded", user, "{}", "{}")
	if err != nil {
		ts.FailNow("failed to update file event log")
	}

	infoFile, err := ts.db.GetFileDetails(context.TODO(), fileID, "uploaded")
	assert.NoError(ts.T(), err, "failed to get user and path from UUID")
	assert.Equal(ts.T(), user, infoFile.User)
	assert.Equal(ts.T(), filePath, infoFile.Path)
}

func (ts *DatabaseTests) TestGetFileDetailsFromUUID_NotFound() {
	// Use a non-existent UUID
	invalidUUID := "abc-123"
	infoFile, err := ts.db.GetFileDetails(context.TODO(), invalidUUID, "uploaded")
	assert.Error(ts.T(), err, "expected error for non-existent UUID")
	assert.Nil(ts.T(), infoFile)
}

func (ts *DatabaseTests) TestSetSubmissionFileSize() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/test.file", "user")
	if err != nil {
		ts.FailNow("failed to register file", err)
	}

	fileSize := int64(time.Now().Nanosecond())
	err = ts.db.SetSubmissionFileSize(context.TODO(), fileID, fileSize)
	if err != nil {
		ts.FailNow("failed to set submission file size", err)
	}

	var sizeInDb int64
	err = ts.verificationDB.QueryRow("SELECT submission_file_size FROM sda.files WHERE id=$1", fileID).Scan(&sizeInDb)
	if err != nil {
		ts.FailNow("failed to get submission file size from DB", err)
	}

	assert.Equal(ts.T(), fileSize, sizeInDb)
}

func (ts *DatabaseTests) TestGetSizeAndObjectCountOfLocation() {
	for _, test := range []struct {
		testName string

		filesToRegister map[string]int64  // file id -> size
		filesToArchive  map[string]int64  // file id -> size
		filesToDataset  map[string]string // file id -> accession id
		locationToQuery string
		expectedCount   uint64
		expectedSize    uint64
	}{
		{
			testName:        "NoData",
			filesToRegister: nil,
			filesToArchive:  nil,
			filesToDataset:  nil,
			locationToQuery: "/inbox",
			expectedSize:    0,
			expectedCount:   0,
		}, {
			testName:        "OnlySubmissionLocationSet",
			filesToRegister: map[string]int64{"00000000-0000-0000-0000-000000000000": 200, "00000000-0000-0000-0000-000000000001": 300},
			filesToArchive:  nil,
			filesToDataset:  nil,
			locationToQuery: "/inbox",
			expectedSize:    200 + 300,
			expectedCount:   2,
		},
		{
			testName:        "SubmissionAndArchiveLocationSet_QueryInbox",
			filesToRegister: map[string]int64{"00000000-0000-0000-0000-000000000000": 200, "00000000-0000-0000-0000-000000000001": 300, "00000000-0000-0000-0000-000000000002": 500, "00000000-0000-0000-0000-000000000004": 600},
			filesToArchive:  map[string]int64{"00000000-0000-0000-0000-000000000000": 224, "00000000-0000-0000-0000-000000000001": 430},
			filesToDataset:  nil,
			locationToQuery: "/inbox",
			expectedSize:    200 + 300 + 500 + 600,
			expectedCount:   4,
		},
		{
			testName:        "SubmissionAndArchiveLocationSet_QueryArchive",
			filesToRegister: map[string]int64{"00000000-0000-0000-0000-000000000000": 200, "00000000-0000-0000-0000-000000000001": 300, "00000000-0000-0000-0000-000000000002": 500, "00000000-0000-0000-0000-000000000004": 600},
			filesToArchive:  map[string]int64{"00000000-0000-0000-0000-000000000000": 224, "00000000-0000-0000-0000-000000000001": 430},
			filesToDataset:  nil,
			locationToQuery: "/archive",
			expectedSize:    224 + 430,
			expectedCount:   2,
		},
		{
			testName:        "SubmissionAndArchiveLocationSetPartlyDataset_QueryInbox",
			filesToRegister: map[string]int64{"00000000-0000-0000-0000-000000000000": 200, "00000000-0000-0000-0000-000000000001": 300, "00000000-0000-0000-0000-000000000002": 500, "00000000-0000-0000-0000-000000000004": 600},
			filesToArchive:  map[string]int64{"00000000-0000-0000-0000-000000000000": 224, "00000000-0000-0000-0000-000000000001": 430, "00000000-0000-0000-0000-000000000002": 550},
			filesToDataset:  map[string]string{"00000000-0000-0000-0000-000000000000": "accession-id-1", "00000000-0000-0000-0000-000000000001": "accession-id-2"},
			locationToQuery: "/inbox",
			expectedSize:    500 + 600,
			expectedCount:   2,
		},
		{
			testName:        "SubmissionAndArchiveLocationSetPartlyDataset_QueryArchive",
			filesToRegister: map[string]int64{"00000000-0000-0000-0000-000000000000": 200, "00000000-0000-0000-0000-000000000001": 300, "00000000-0000-0000-0000-000000000002": 500, "00000000-0000-0000-0000-000000000004": 600},
			filesToArchive:  map[string]int64{"00000000-0000-0000-0000-000000000000": 224, "00000000-0000-0000-0000-000000000001": 430, "00000000-0000-0000-0000-000000000002": 550},
			filesToDataset:  map[string]string{"00000000-0000-0000-0000-000000000000": "accession-id-1", "00000000-0000-0000-0000-000000000001": "accession-id-2"},
			locationToQuery: "/archive",
			expectedSize:    224 + 430 + 550,
			expectedCount:   3,
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			// Clean data
			_, err := ts.verificationDB.Exec("DELETE FROM sda.file_event_log")
			assert.NoError(t, err)
			_, err = ts.verificationDB.Exec("DELETE FROM sda.file_dataset")
			assert.NoError(t, err)
			_, err = ts.verificationDB.Exec("DELETE FROM sda.dataset_event_log")
			assert.NoError(t, err)
			_, err = ts.verificationDB.Exec("DELETE FROM sda.datasets")
			assert.NoError(t, err)
			_, err = ts.verificationDB.Exec("DELETE FROM sda.checksums")
			assert.NoError(t, err)
			_, err = ts.verificationDB.Exec("DELETE FROM sda.files")
			assert.NoError(t, err)

			for fileID, size := range test.filesToRegister {
				_, err := ts.db.RegisterFile(context.TODO(), &fileID, "/inbox", "/"+fileID, "user")
				assert.NoError(t, err)
				assert.NoError(t, ts.db.SetSubmissionFileSize(context.TODO(), fileID, size))
			}
			for fileID, size := range test.filesToArchive {
				assert.NoError(t, ts.db.SetArchived(context.TODO(), "/archive", &database.FileInfo{
					ArchivedChecksum:  "123",
					Size:              size,
					Path:              "/test.file3",
					DecryptedChecksum: "321",
					DecryptedSize:     size,
					UploadedChecksum:  "abc",
				}, fileID))
			}
			if len(test.filesToDataset) > 0 {
				for fileID, accessionID := range test.filesToDataset {
					assert.NoError(t, ts.db.SetAccessionID(context.TODO(), accessionID, fileID))
					assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "unit-test-dataset-id", fileID))
				}
			}

			size, count, err := ts.db.GetSizeAndObjectCountOfLocation(context.TODO(), test.locationToQuery)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedSize, size)
			assert.Equal(t, test.expectedCount, count)
		})
	}
}

func (ts *DatabaseTests) TestCancelFile() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/test.file", "user")
	if err != nil {
		ts.FailNow("failed to register file", err)
	}

	assert.NoError(ts.T(), ts.db.SetArchived(context.TODO(), "/archive", &database.FileInfo{
		ArchivedChecksum:  "123",
		Size:              500,
		Path:              "/test.file3",
		DecryptedChecksum: "321",
		DecryptedSize:     550,
		UploadedChecksum:  "abc",
	}, fileID))

	assert.NoError(ts.T(), ts.db.CancelFile(context.TODO(), fileID, "{}"))

	// Check that data has been unset
	archiveData, err := ts.db.GetArchived(context.TODO(), fileID)
	assert.NoError(ts.T(), err)
	assert.Nil(ts.T(), archiveData)
}

func (ts *DatabaseTests) TestIsFileInDataset_No() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/test.file", "user")
	if err != nil {
		ts.FailNow("failed to register file", err)
	}

	inDataset, err := ts.db.IsFileInDataset(context.TODO(), fileID)
	assert.NoError(ts.T(), err)
	assert.False(ts.T(), inDataset)
}

func (ts *DatabaseTests) TestIsFileInDataset_Yes() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/test.file", "user")
	if err != nil {
		ts.FailNow("failed to register file", err)
	}
	assert.NoError(ts.T(), ts.db.SetArchived(context.TODO(), "/archive", &database.FileInfo{
		ArchivedChecksum:  "123",
		Size:              500,
		Path:              "/test.file3",
		DecryptedChecksum: "321",
		DecryptedSize:     550,
		UploadedChecksum:  "abc",
	}, fileID))

	assert.NoError(ts.T(), ts.db.SetAccessionID(context.TODO(), "accessionID-1", fileID))
	assert.NoError(ts.T(), ts.db.MapFileToDataset(context.TODO(), "unit-test-dataset-id", fileID))

	inDataset, err := ts.db.IsFileInDataset(context.TODO(), fileID)
	assert.NoError(ts.T(), err)
	assert.True(ts.T(), inDataset)
}

func (ts *DatabaseTests) TestSetBackedUp() {
	// register a file in the database
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "/testuser/TestSetArchived.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	assert.NoError(ts.T(), ts.db.SetArchived(context.TODO(), "/archive", &database.FileInfo{
		Size:              1000,
		Path:              fileID,
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     999,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}, fileID))

	assert.NoError(ts.T(), ts.db.SetBackedUp(context.TODO(), "/backup", fileID, fileID))

	// Ensure backup_location and backup_path are set
	archiveData, err := ts.db.GetArchived(context.TODO(), fileID)

	assert.NoError(ts.T(), err)
	if archiveData == nil {
		ts.FailNow("archive data not found")

		return
	}

	ts.Equal("/backup", archiveData.BackupLocation)
	ts.Equal(fileID, archiveData.BackupFilePath)
}
func (ts *DatabaseTests) TestSetBackedUp_FileID_Not_Exists() {
	notExistingFileID := uuid.NewString()
	assert.EqualError(ts.T(), ts.db.SetBackedUp(context.TODO(), "/backup", notExistingFileID, notExistingFileID), sql.ErrNoRows.Error())
}

func (ts *DatabaseTests) TestGetFileIDInInbox() {
	fileID, err := ts.db.RegisterFile(context.TODO(), nil, "/inbox", "TestGetFileIDInInbox.c4gh", "testuser")
	assert.NoError(ts.T(), err, "failed to register file in database")

	fileIDFromDB, err := ts.db.GetFileIDInInbox(context.TODO(), "testuser", "TestGetFileIDInInbox.c4gh")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), fileID, fileIDFromDB)

	assert.NoError(ts.T(), ts.db.SetArchived(context.TODO(), "/archive", &database.FileInfo{
		Size:              1000,
		Path:              fileID,
		ArchivedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedChecksum: fmt.Sprintf("%x", sha256.New().Sum(nil)),
		DecryptedSize:     -1,
		UploadedChecksum:  fmt.Sprintf("%x", sha256.New().Sum(nil)),
	}, fileID))

	fileIDFromDB, err = ts.db.GetFileIDInInbox(context.TODO(), "testuser", "TestGetFileIDInInbox.c4gh")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "", fileIDFromDB)

	assert.NoError(ts.T(), ts.db.CancelFile(context.TODO(), fileID, "{}"))

	fileIDFromDB, err = ts.db.GetFileIDInInbox(context.TODO(), "testuser", "TestGetFileIDInInbox.c4gh")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), fileID, fileIDFromDB)
}

func (ts *DatabaseTests) TestGetFileIDInInbox_NotFound() {
	fileIDFromDB, err := ts.db.GetFileIDInInbox(context.TODO(), "testuser", "TestGetFileIDInInbox.c4gh")
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "", fileIDFromDB)
}
