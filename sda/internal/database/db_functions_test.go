package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/stretchr/testify/assert"
)

// TestRegisterFile tests that RegisterFile() behaves as intended
func (suite *DatabaseTests) TestRegisterFile() {
	// create database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/file1.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	// check that the returning fileID is a uuid
	uuidPattern := "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	r := regexp.MustCompile(uuidPattern)
	assert.True(suite.T(), r.MatchString(fileID), "RegisterFile() did not return a valid UUID: "+fileID)

	// check that the file is in the database
	exists := false
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.files WHERE id=$1)", fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "Failed to check if registered file exists")
	assert.True(suite.T(), exists, "RegisterFile() did not insert a row into sda.files with id: "+fileID)

	// check that there is a "registered" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='registered')", fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "Failed to check if registered file event exists")
	assert.True(suite.T(), exists, "RegisterFile() did not insert a row into sda.file_event_log with id: "+fileID)

	db.Close()
}

func (suite *DatabaseTests) TestRegisterFileWithID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	insertedFileID := uuid.New().String()
	fileID, err := db.RegisterFile(&insertedFileID, "/testuser/file3.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.UpdateFileEventLog(fileID, "uploaded", "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update file status")

	fID, err := db.GetFileIDByUserPathAndStatus("testuser", "/testuser/file3.c4gh", "uploaded")
	assert.NoError(suite.T(), err, "GetFileId failed")
	assert.Equal(suite.T(), insertedFileID, fileID)
	assert.Equal(suite.T(), fileID, fID)

	db.Close()
}

func (suite *DatabaseTests) TestUpdateFileEventLog() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/file4.c4gh", "testuser")
	assert.Nil(suite.T(), err, "failed to register file in database")

	// Attempt to mark a file that doesn't exist as uploaded
	err = db.UpdateFileEventLog("00000000-0000-0000-0000-000000000000", "uploaded", "testuser", "{}", "{}")
	assert.NotNil(suite.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = db.UpdateFileEventLog(fileID, "uploaded", "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to set file as uploaded in database")

	exists := false
	// check that there is an "uploaded" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='uploaded')", fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "Failed to check if uploaded file event exists")
	assert.True(suite.T(), exists, "UpdateFileEventLog() did not insert a row into sda.file_event_log with id: "+fileID)

	db.Close()
}

func (suite *DatabaseTests) TestStoreHeader() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestStoreHeader.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	// store header for non existing entry
	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, "00000000-0000-0000-0000-000000000000")
	assert.EqualError(suite.T(), err, "something went wrong with the query zero rows were changed")

	db.Close()
}

func (suite *DatabaseTests) TestRotateHeaderKey() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// Register a new key and a new file
	fileID, err := db.RegisterFile(nil, "/testuser/file1.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = db.addKeyHash("someKeyHash", "this is a test key")
	assert.NoError(suite.T(), err, "failed to register key in database")
	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	// test happy path
	newKeyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507`
	err = db.addKeyHash(newKeyHex, "new key")
	assert.NoError(suite.T(), err, "failed to register key in database")
	newHHeader := []byte{1, 2, 3}

	err = db.RotateHeaderKey(newHHeader, newKeyHex, fileID)
	assert.NoError(suite.T(), err)

	// Verify that the key+header were updated
	var dbHeaderString, dbKeyHash string
	err = db.DB.QueryRow("SELECT header, key_hash FROM sda.files WHERE id=$1", fileID).Scan(&dbHeaderString, &dbKeyHash)
	assert.NoError(suite.T(), err)
	dbHeader, err := hex.DecodeString(dbHeaderString)
	assert.NoError(suite.T(), err, "hex decoding of rotated header failed")
	assert.Equal(suite.T(), newHHeader, dbHeader)
	assert.Equal(suite.T(), newKeyHex, dbKeyHash)

	// case of non registered keyhash
	err = db.RotateHeaderKey([]byte{2, 4, 6, 8}, "unknownKeyHash", fileID)
	assert.ErrorContains(suite.T(), err, "violates foreign key constraint")
	// check that no column was updated
	err = db.DB.QueryRow("SELECT header, key_hash FROM sda.files WHERE id=$1", fileID).Scan(&dbHeaderString, &dbKeyHash)
	assert.NoError(suite.T(), err)
	dbHeader, err = hex.DecodeString(dbHeaderString)
	assert.NoError(suite.T(), err, "hex decoding of rotated header failed")
	assert.Equal(suite.T(), newHHeader, dbHeader)
	assert.Equal(suite.T(), newKeyHex, dbKeyHash)

	// case of non existing entry
	err = db.RotateHeaderKey([]byte{15, 45, 20, 40, 48}, "keyHex", "00000000-0000-0000-0000-000000000000")
	assert.EqualError(suite.T(), err, "something went wrong with the query zero rows were changed")

	db.Close()
}

func (suite *DatabaseTests) TestSetArchived() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestSetArchived.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetArchived.c4gh", fmt.Sprintf("%x", sha256.New()), -1, fmt.Sprintf("%x", sha256.New())}
	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetArchived(fileInfo, "00000000-0000-0000-0000-000000000000")
	assert.ErrorContains(suite.T(), err, "violates foreign key constraint")

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	db.Close()
}

func (suite *DatabaseTests) TestGetFileStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetFileStatus.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.UpdateFileEventLog(fileID, "downloaded", "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to set file as downloaded in database")

	status, err := db.GetFileStatus(fileID)
	assert.NoError(suite.T(), err, "failed to get file status")
	assert.Equal(suite.T(), "downloaded", status)

	db.Close()
}

func (suite *DatabaseTests) TestGetHeader() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetHeader.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	header, err := db.GetHeader(fileID)
	assert.NoError(suite.T(), err, "failed to get file header")
	assert.Equal(suite.T(), []byte{15, 45, 20, 40, 48}, header)

	db.Close()
}

func (suite *DatabaseTests) TestSetVerified() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestSetVerified.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/testuser/TestSetVerified.c4gh", fmt.Sprintf("%x", sha256.New()), 948, fmt.Sprintf("%x", sha256.New())}
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	db.Close()
}

func (suite *DatabaseTests) TestGetArchived() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetArchived.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestGetArchived.c4gh", fmt.Sprintf("%x", sha256.New()), 987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	filePath, fileSize, err := db.GetArchived(fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), 1000, fileSize)
	assert.Equal(suite.T(), "/tmp/TestGetArchived.c4gh", filePath)

	db.Close()
}

func (suite *DatabaseTests) TestSetAccessionID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetAccessionID.c4gh", fmt.Sprintf("%x", sha256.New()), 987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
	stableID := "TEST:000-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	db.Close()
}

func (suite *DatabaseTests) TestCheckAccessionIDExists() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestCheckAccessionIDExists.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestCheckAccessionIDExists.c4gh", fmt.Sprintf("%x", sha256.New()), 987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
	stableID := "TEST:111-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	exists, err := db.CheckAccessionIDExists(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), "same", exists)

	duplicate, err := db.CheckAccessionIDExists(stableID, uuid.New().String())
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), "duplicate", duplicate)

	db.Close()
}

func (suite *DatabaseTests) TestGetAccessionID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetAccessionID.c4gh", fmt.Sprintf("%x", sha256.New()), 987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
	stableID := "TEST:000-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	res, err := db.GetAccessionID(fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting accessionID of file", err)
	assert.Equal(suite.T(), stableID, res, "retrieved accessionID is wrong")

	db.Close()
}

func (suite *DatabaseTests) TestGetAccessionID_wrongFileID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetAccessionID.c4gh", fmt.Sprintf("%x", sha256.New()), 987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
	stableID := "TEST:000-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	// locally reduce RetryTimes to avoid 30s waiting limit of testsuite
	RetryTimes = 2

	// check for bad format
	_, err = db.GetAccessionID("someFileID")
	assert.ErrorContains(suite.T(), err, "invalid input syntax for type uuid")

	// check for non-existent fileID
	_, err = db.GetAccessionID(uuid.New().String())
	assert.ErrorContains(suite.T(), err, "no rows in result set")

	db.Close()
}

func (suite *DatabaseTests) TestGetFileInfo() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetFileInfo.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/tmp/TestGetFileInfo.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	info, err := db.GetFileInfo(fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), int64(2000), info.Size)
	assert.Equal(suite.T(), "/tmp/TestGetFileInfo.c4gh", info.Path)
	assert.Equal(suite.T(), "11c94bc7fb13afeb2b3fb16c1dbe9206dc09560f1b31420f2d46210ca4ded0a8", info.ArchiveChecksum)
	assert.Equal(suite.T(), "a671218c2418aa51adf97e33c5c91a720289ba3c9fd0d36f6f4bf9610730749f", info.DecryptedChecksum)

	db.Close()
}

func (suite *DatabaseTests) TestMapFilesToDataset() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 1; i < 12; i++ {
		fileID, err := db.RegisterFile(nil, fmt.Sprintf("/testuser/TestMapFilesToDataset-%d.c4gh", i), "testuser")
		assert.NoError(suite.T(), err, "failed to register file in database")

		err = db.SetAccessionID(fmt.Sprintf("acession-%d", i), fileID)
		assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fmt.Sprintf("acession-%d", i))
	}

	diSet := map[string][]string{
		"dataset1": accessions[0:3],
		"dataset2": accessions[3:5],
		"dataset3": accessions[5:8],
		"dataset4": accessions[8:9],
	}

	for di, acs := range diSet {
		err := db.MapFilesToDataset(di, acs)
		assert.NoError(suite.T(), err, "failed to map file to dataset")
	}

	// Append files to an existing dataset
	assert.NoError(suite.T(), db.MapFilesToDataset("dataset1", accessions[9:11]), "failed to append files to a dataset")
	var dsMembers int
	const q1 = "SELECT count(file_id) from sda.file_dataset WHERE dataset_id = (SELECT id from sda.datasets WHERE stable_id = $1);"
	if err := db.DB.QueryRow(q1, "dataset1").Scan(&dsMembers); err != nil {
		suite.FailNow("failed to get dataset members from database")
	}
	assert.Equal(suite.T(), 5, dsMembers)

	db.Close()
}

func (suite *DatabaseTests) TestGetInboxPath() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := db.RegisterFile(nil, fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
		assert.NoError(suite.T(), err, "failed to register file in database")

		err = db.SetAccessionID(fmt.Sprintf("acession-00%d", i), fileID)
		assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fmt.Sprintf("acession-00%d", i))
	}

	for _, acc := range accessions {
		path, err := db.getInboxPath(acc)
		assert.NoError(suite.T(), err, "getInboxPath failed")
		assert.Contains(suite.T(), path, "/testuser/TestGetInboxPath")
	}

	db.Close()
}

func (suite *DatabaseTests) TestUpdateDatasetEvent() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := db.RegisterFile(nil, fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
		assert.NoError(suite.T(), err, "failed to register file in database")

		err = db.SetAccessionID(fmt.Sprintf("acession-00%d", i), fileID)
		assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fmt.Sprintf("acession-00%d", i))
	}

	diSet := map[string][]string{"DATASET:TEST-0001": accessions}

	for di, acs := range diSet {
		err := db.MapFilesToDataset(di, acs)
		assert.NoError(suite.T(), err, "failed to map file to dataset")
	}

	dID := "DATASET:TEST-0001"
	err = db.UpdateDatasetEvent(dID, "registered", "{\"type\": \"mapping\"}")
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	err = db.UpdateDatasetEvent(dID, "released", "{\"type\": \"release\"}")
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	err = db.UpdateDatasetEvent(dID, "deprecated", "{\"type\": \"deprecate\"}")
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	db.Close()
}

func (suite *DatabaseTests) TestGetHeaderForStableID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetHeaderForStableID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte("HEADER"), fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	stableID := "TEST:010-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when setting stable ID: %s, %s", err, stableID, fileID)

	header, err := db.GetHeaderForStableID("TEST:010-1234-4567")
	assert.NoError(suite.T(), err, "failed to get header for stable ID: %v", err)
	assert.Equal(suite.T(), header, []byte("HEADER"), "did not get expected header")

	db.Close()
}

func (suite *DatabaseTests) TestGetSyncData() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile(nil, "/testuser/TestGetGetSyncData.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, "/tmp/TestGetGetSyncData.c4gh", checksum, 999, fmt.Sprintf("%x", sha256.New())}

	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetVerified(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Verified")

	stableID := "TEST:000-1111-2222"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when setting stable ID: %s, %s", err, stableID, fileID)

	fileData, err := db.getSyncData("TEST:000-1111-2222")
	assert.NoError(suite.T(), err, "failed to get sync data for file")
	assert.Equal(suite.T(), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", fileData.Checksum, "did not get expected file checksum")
	assert.Equal(suite.T(), "/testuser/TestGetGetSyncData.c4gh", fileData.FilePath, "did not get expected file path")
	assert.Equal(suite.T(), "testuser", fileData.User, "did not get expected user")

	db.Close()
}

func (suite *DatabaseTests) TestCheckIfDatasetExists() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	accessions := []string{}
	for i := 0; i <= 3; i++ {
		fileID, err := db.RegisterFile(nil, fmt.Sprintf("/testuser/TestCheckIfDatasetExists-%d.c4gh", i), "testuser")
		assert.NoError(suite.T(), err, "failed to register file in database")

		err = db.SetAccessionID(fmt.Sprintf("accession-%d", i), fileID)
		assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

		accessions = append(accessions, fmt.Sprintf("accession-%d", i))
	}

	diSet := map[string][]string{
		"dataset": accessions[0:3],
	}

	for di, acs := range diSet {
		err := db.MapFilesToDataset(di, acs)
		assert.NoError(suite.T(), err, "failed to map file to dataset")
	}

	ok, err := db.checkIfDatasetExists("dataset")
	assert.NoError(suite.T(), err, "check if dataset exists failed")
	assert.Equal(suite.T(), ok, true)

	ok, err = db.checkIfDatasetExists("missing dataset")
	assert.NoError(suite.T(), err, "check if dataset exists failed")
	assert.Equal(suite.T(), ok, false)

	db.Close()
}

func (suite *DatabaseTests) TestGetArchivePath() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile(nil, "/testuser/TestGetArchivePath-001.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	checksum := fmt.Sprintf("%x", sha256.New())
	corrID := uuid.New().String()
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1234, corrID, checksum, 999, fmt.Sprintf("%x", sha256.New())}
	err = db.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetAccessionID("acession-0001", fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	path, err := db.getArchivePath("acession-0001")
	assert.NoError(suite.T(), err, "getArchivePath failed")
	assert.Equal(suite.T(), path, corrID)

	db.Close()
}

func (suite *DatabaseTests) TestGetUserFiles() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5
	testUser := "GetFilesUser"
	sub := "submission_a"

	for i := range testCases {
		if i == 2 {
			sub = "submission_b"
		}

		fileID, err := db.RegisterFile(nil, fmt.Sprintf("%v/%s/TestGetUserFiles-00%d.c4gh", testUser, sub, i), testUser)
		assert.NoError(suite.T(), err, "failed to register file in database")
		err = db.UpdateFileEventLog(fileID, "uploaded", testUser, "{}", "{}")
		assert.NoError(suite.T(), err, "failed to update satus of file in database")
		err = db.SetAccessionID(fmt.Sprintf("stableID-00%d", i), fileID)
		assert.NoError(suite.T(), err, "failed to update satus of file in database")
		err = db.UpdateFileEventLog(fileID, "ready", testUser, "{}", "{}")
		assert.NoError(suite.T(), err, "failed to update satus of file in database")
	}
	filelist, err := db.GetUserFiles("unknownuser", "", true)
	assert.NoError(suite.T(), err, "failed to get (empty) file list of unknown user")
	assert.Empty(suite.T(), filelist, "file list of unknown user is not empty")

	filelist, err = db.GetUserFiles(testUser, "", true)
	assert.NoError(suite.T(), err, "failed to get file list")
	assert.Equal(suite.T(), testCases, len(filelist), "file list is of incorrect length")

	for _, fileInfo := range filelist {
		assert.Equal(suite.T(), "ready", fileInfo.Status, "incorrect file status")
		assert.Contains(suite.T(), fileInfo.AccessionID, "stableID-00", "incorrect file accession ID")
	}

	filteredFilelist, err := db.GetUserFiles(testUser, fmt.Sprintf("%s/submission_b", testUser), true)
	assert.NoError(suite.T(), err, "failed to get file list")
	assert.Equal(suite.T(), 3, len(filteredFilelist), "file list is of incorrect length")

	db.Close()
}

func (suite *DatabaseTests) TestGetCorrID_sameFilePath() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	filePath := "/testuser/file10.c4gh"
	user := "testuser"

	fileID, err := db.RegisterFile(nil, filePath, user)
	if err != nil {
		suite.FailNow("failed to register file in database")
	}
	if err := db.UpdateFileEventLog(fileID, "archived", user, "{}", "{}"); err != nil {
		suite.FailNow("failed to update satus of file in database")
	}

	checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, fileID, checksum, 999, fmt.Sprintf("%x", sha256.New())}
	if err := db.SetArchived(fileInfo, fileID); err != nil {
		suite.FailNow("failed to mark file as archived")
	}
	if err := db.UpdateFileEventLog(fileID, "archived", user, "{}", "{}"); err != nil {
		suite.FailNow("failed to update satus of file in database")
	}
	if err = db.SetAccessionID("stableID", fileID); err != nil {
		suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "stableID", fileID)
	}

	fileID2, err := db.RegisterFile(nil, filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	if err := db.UpdateFileEventLog(fileID2, "uploaded", user, "{}", "{}"); err != nil {
		suite.FailNow("failed to update satus of file in database")
	}
	assert.NotEqual(suite.T(), fileID, fileID2)

	db.Close()
}

func (suite *DatabaseTests) TestListActiveUsers() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5
	testUsers := []string{"User-A", "User-B", "User-C", "User-D"}

	for _, user := range testUsers {
		for i := 0; i < testCases; i++ {
			filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i)
			fileID, err := db.RegisterFile(nil, filePath, user)
			if err != nil {
				suite.FailNow("Failed to register file")
			}
			err = db.UpdateFileEventLog(fileID, "uploaded", user, "{}", "{}")
			if err != nil {
				suite.FailNow("Failed to update file event log")
			}

			checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
			fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, filePath, checksum, 999, fmt.Sprintf("%x", sha256.New())}
			err = db.SetArchived(fileInfo, fileID)
			if err != nil {
				suite.FailNow("failed to mark file as Archived")
			}

			err = db.SetVerified(fileInfo, fileID)
			if err != nil {
				suite.FailNow("failed to mark file as Verified")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = db.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}

		db.Close()
	}

	err = db.MapFilesToDataset("test-dataset-01", []string{"accession_User-A_00", "accession_User-A_01", "accession_User-A_02"})
	if err != nil {
		suite.FailNow("failed to map files§ to dataset")
	}

	err = db.MapFilesToDataset("test-dataset-02", []string{"accession_User-C_00", "accession_User-C_01", "accession_User-C_02", "accession_User-C_03", "accession_User-C_04"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	userList, err := db.ListActiveUsers()
	assert.NoError(suite.T(), err, "failed to list users from DB")
	assert.Equal(suite.T(), 3, len(userList))

	db.Close()
}

func (suite *DatabaseTests) TestGetDatasetStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", "User-Q", i)
		fileID, err := db.RegisterFile(nil, filePath, "User-Q")
		if err != nil {
			suite.FailNow("Failed to register file")
		}
		err = db.UpdateFileEventLog(fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			suite.FailNow("Failed to update file event log")
		}

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		fileInfo := FileInfo{
			fmt.Sprintf("%x", sha256.New().Sum(nil)),
			1234,
			filePath,
			checksum,
			999,
			fmt.Sprintf("%x", sha256.New()),
		}
		err = db.SetArchived(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		err = db.SetVerified(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = db.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	dID := "test-get-dataset-01"
	if err := db.MapFilesToDataset(dID, []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	err = db.UpdateDatasetEvent(dID, "registered", "{\"type\": \"mapping\"}")
	assert.NoError(suite.T(), err, "got (%v) when updating dataset event", err)
	status, err := db.GetDatasetStatus(dID)
	assert.NoError(suite.T(), err, "got (%v) when no error weas expected")
	assert.Equal(suite.T(), "registered", status)

	err = db.UpdateDatasetEvent(dID, "released", "{\"type\": \"mapping\"}")
	assert.NoError(suite.T(), err, "got (%v) when updating dataset event", err)
	status, err = db.GetDatasetStatus(dID)
	assert.NoError(suite.T(), err, "got (%v) when no error weas expected")
	assert.Equal(suite.T(), "released", status)

	err = db.UpdateDatasetEvent(dID, "deprecated", "{\"type\": \"mapping\"}")
	assert.NoError(suite.T(), err, "got (%v) when updating dataset event", err)
	status, err = db.GetDatasetStatus(dID)
	assert.NoError(suite.T(), err, "got (%v) when no error weas expected")
	assert.Equal(suite.T(), "deprecated", status)

	db.Close()
}

func (suite *DatabaseTests) TestAddKeyHash() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// Test registering a new key and its description
	keyHex := `cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23`
	keyDescription := "this is a test key"
	err = db.AddKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")

	// Verify that the key was added
	var exists bool
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.encryption_keys WHERE key_hash=$1 AND description=$2)", keyHex, keyDescription).Scan(&exists)
	assert.NoError(suite.T(), err, "failed to verify key hash existence")
	assert.True(suite.T(), exists, "key hash was not added to the database")

	db.Close()
}

func (suite *DatabaseTests) TestListKeyHashes() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")
	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99", "this is a another key"), "failed to register key in database")

	expectedResponse := C4ghKeyHash{
		Hash:         "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23",
		Description:  "this is a test key",
		CreatedAt:    time.Now().UTC().Format(time.DateOnly),
		DeprecatedAt: "",
	}
	hashList, err := db.ListKeyHashes()
	ct, _ := time.Parse(time.RFC3339, hashList[0].CreatedAt)
	hashList[0].CreatedAt = ct.Format(time.DateOnly)
	assert.NoError(suite.T(), err, "failed to verify key hash existence")
	assert.Equal(suite.T(), expectedResponse, hashList[0], "key hash was not added to the database")

	db.Close()
}

func (suite *DatabaseTests) TestListKeyHashes_emptyTable() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	hashList, err := db.ListKeyHashes()
	assert.NoError(suite.T(), err, "failed to verify key hash existence")
	assert.Equal(suite.T(), []C4ghKeyHash{}, hashList, "fuu")

	db.Close()
}

func (suite *DatabaseTests) TestDeprecateKeyHashes() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc32", "this is a test key"), "failed to register key in database")

	assert.NoError(suite.T(), db.DeprecateKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc32"), "failure when deprecating keyhash")

	db.Close()
}

func (suite *DatabaseTests) TestDeprecateKeyHashes_wrongHash() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc11", "this is a another key"), "failed to register key in database")

	assert.EqualError(suite.T(), db.DeprecateKeyHash("wr0n6h4sh"), "key hash not found or already deprecated", "failure when deprecating non existing keyhash")

	db.Close()
}

func (suite *DatabaseTests) TestDeprecateKeyHashes_alreadyDeprecated() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54", "this is a test key"), "failed to register key in database")

	assert.NoError(suite.T(), db.DeprecateKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54"), "failure when deprecating keyhash")

	// we should not be able to change the deprecation date
	assert.EqualError(suite.T(), db.DeprecateKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc54"), "key hash not found or already deprecated", "failure when deprecating keyhash")

	db.Close()
}

func (suite *DatabaseTests) TestSetKeyHash() {
	// Test that using an unknown key hash produces an error
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc507`
	keyDescription := "this is a test key"
	err = db.addKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")
	fileID, err := db.RegisterFile(nil, "/testuser/file1.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	// Test that the key hash can be set in the files table
	err = db.SetKeyHash(keyHex, fileID)
	assert.NoError(suite.T(), err, "Could not set key hash")

	// Verify that the key+file was added
	var exists bool
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.files WHERE key_hash=$1 AND id=$2)", keyHex, fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "failed to verify key hash set for file")
	assert.True(suite.T(), exists, "key hash was not set for file in the database")

	db.Close()
}

func (suite *DatabaseTests) TestSetKeyHash_wrongHash() {
	// Add key hash and file
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	keyHex := "6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc501"
	keyDescription := "this is a test hash"
	err = db.addKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")
	fileID, err := db.RegisterFile(nil, "/testuser/file2.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	// Ensure failure if a non existing hash is used
	newKeyHex := "6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc502"
	err = db.SetKeyHash(newKeyHex, fileID)
	assert.ErrorContains(suite.T(), err, "violates foreign key constraint")

	db.Close()
}

func (suite *DatabaseTests) TestGetKeyHash() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc509`
	keyDescription := "this is a test key"
	err = db.addKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")
	fileID, err := db.RegisterFile(nil, "/testuser/file1.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = db.SetKeyHash(keyHex, fileID)
	assert.NoError(suite.T(), err, "failed to set key hash in database")

	// Test happy path
	keyHash, err := db.GetKeyHash(fileID)
	assert.NoError(suite.T(), err, "Could not get key hash")
	assert.Equal(suite.T(), keyHex, keyHash)

	db.Close()
}

func (suite *DatabaseTests) TestGetKeyHash_wrongFileID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	// Register a new key and a new file
	keyHex := `6af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc509`
	keyDescription := "this is a test key"
	err = db.addKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")
	fileID, err := db.RegisterFile(nil, "/testuser/file1.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = db.SetKeyHash(keyHex, fileID)
	assert.NoError(suite.T(), err, "failed to set key hash in database")

	// Test that using an unknown fileID produces an error
	_, err = db.GetKeyHash("097e1dc9-6b42-42bf-966d-dece6fefda09")
	assert.ErrorContains(suite.T(), err, "no rows in result set")

	db.Close()
}

func (suite *DatabaseTests) TestCheckKeyHash() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")
	anotherKeyhash := "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99"
	assert.NoError(suite.T(), db.AddKeyHash(anotherKeyhash, "this is a another key"), "failed to register key in database")

	err = db.CheckKeyHash(anotherKeyhash)
	assert.NoError(suite.T(), err, "failed to verify active key hash lookup")

	db.Close()
}

func (suite *DatabaseTests) TestCheckKeyHash_keyDeprecated() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")
	anotherKeyhash := "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99"
	assert.NoError(suite.T(), db.AddKeyHash(anotherKeyhash, "this is a another key"), "failed to register key in database")
	assert.NoError(suite.T(), db.DeprecateKeyHash(anotherKeyhash), "failure when deprecating keyhash")

	err = db.CheckKeyHash(anotherKeyhash)
	assert.ErrorContains(suite.T(), err, "the c4gh key hash has been deprecated")

	db.Close()
}

func (suite *DatabaseTests) TestCheckKeyHash_keyNonExistent() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	assert.NoError(suite.T(), db.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")

	err = db.CheckKeyHash("somekeyhash")
	assert.ErrorContains(suite.T(), err, "the c4gh key hash is not registered")

	db.Close()
}

func (suite *DatabaseTests) TestListDatasets() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 10

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", "User-Q", i)
		fileID, err := db.RegisterFile(nil, filePath, "User-Q")
		if err != nil {
			suite.FailNow("Failed to register file")
		}
		err = db.UpdateFileEventLog(fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			suite.FailNow("Failed to update file event log")
		}

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		fileInfo := FileInfo{
			fmt.Sprintf("%x", sha256.New().Sum(nil)),
			1234,
			filePath,
			checksum,
			999,
			fmt.Sprintf("%x", sha256.New()),
		}
		err = db.SetArchived(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		err = db.SetVerified(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = db.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	if err := db.MapFilesToDataset("test-get-dataset-01", []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-01", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-01", "released", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	if err := db.MapFilesToDataset("test-get-dataset-02", []string{"accession_User-Q_03", "accession_User-Q_04", "accession_User-Q_05"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-02", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	if err := db.MapFilesToDataset("test-get-dataset-03", []string{"accession_User-Q_06", "accession_User-Q_07", "accession_User-Q_08"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-03", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-03", "released", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-get-dataset-03", "deprecated", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	datasets, err := db.ListDatasets()
	assert.NoError(suite.T(), err, "got (%v) when listing datasets", err)
	assert.Equal(suite.T(), "test-get-dataset-01", datasets[0].DatasetID)
	assert.Equal(suite.T(), "registered", datasets[1].Status)

	db.Close()
}

func (suite *DatabaseTests) TestListUserDatasets() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	user := "User-Q"
	for i := 0; i < 6; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i)
		fileID, err := db.RegisterFile(nil, filePath, user)
		if err != nil {
			suite.FailNow("Failed to register file")
		}
		err = db.UpdateFileEventLog(fileID, "uploaded", user, "{}", "{}")
		if err != nil {
			suite.FailNow("Failed to update file event log")
		}

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		fileInfo := FileInfo{
			fmt.Sprintf("%x", sha256.New().Sum(nil)),
			1234,
			filePath,
			checksum,
			999,
			fmt.Sprintf("%x", sha256.New()),
		}
		err = db.SetArchived(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		err = db.SetVerified(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", user, i)
		err = db.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}
	if err := db.MapFilesToDataset("test-user-dataset-01", []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-user-dataset-01", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-user-dataset-01", "released", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	if err := db.MapFilesToDataset("test-user-dataset-02", []string{"accession_User-Q_03", "accession_User-Q_04", "accession_User-Q_05"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-user-dataset-02", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	fileID, err := db.RegisterFile(nil, "filePath", "user")
	if err != nil {
		suite.FailNow("Failed to register file")
	}

	err = db.SetAccessionID("stableID", fileID)
	if err != nil {
		suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "stableID", fileID)
	}
	if err := db.MapFilesToDataset("test-wrong-user-dataset", []string{"stableID"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err = db.UpdateDatasetEvent("test-wrong-user-dataset", "registered", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-wrong-user-dataset", "released", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err = db.UpdateDatasetEvent("test-wrong-user-dataset", "deprecated", "{\"type\": \"mapping\"}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	datasets, err := db.ListUserDatasets(user)
	assert.NoError(suite.T(), err, "got (%v) when listing datasets for a user", err)
	assert.Equal(suite.T(), 2, len(datasets))
	assert.Equal(suite.T(), "test-user-dataset-01", datasets[0].DatasetID)

	db.Close()
}

func (suite *DatabaseTests) TestUpdateUserInfo() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// Insert a userID
	var groups []string
	userID, name, email := "12334556testuser@lifescience.ru", "Test User", "test.user@example.org"
	err = db.UpdateUserInfo(userID, name, email, groups)
	assert.NoError(suite.T(), err, "could not insert user info: %v", err)
	// Verify that the userID is connected to the details
	var numRows int
	err = db.DB.QueryRow("SELECT COUNT(*) FROM sda.userinfo WHERE id=$1", userID).Scan(&numRows)
	assert.NoError(suite.T(), err, "could select user info: %v", err)
	assert.Equal(suite.T(), 1, numRows, "there should be exactly 1 row about %v in userinfo table", userID)
	var name2 string
	err = db.DB.QueryRow("SELECT name FROM sda.userinfo WHERE id=$1", userID).Scan(&name2)
	assert.NoError(suite.T(), err, "could not select user info: %v", err)
	assert.Equal(suite.T(), name, name2, "user info table did not update correctly")

	db.Close()
}

func (suite *DatabaseTests) TestUpdateUserInfo_newInfo() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// Insert a user
	var groups []string
	userID, name, email := "12334556testuser@lifescience.ru", "Test User", "test.user@example.org"
	err = db.UpdateUserInfo(userID, name, email, groups)
	assert.NoError(suite.T(), err, "could not insert user info: %v", err)
	var exists bool
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.userinfo WHERE id=$1)", userID).Scan(&exists)
	assert.NoError(suite.T(), err, "failed to verify user info existence")
	assert.True(suite.T(), exists, "user info was not added to the database")

	// Insert new information about the user and verify that there is still only 1 row,
	// and that this row is updated
	var numRows int
	err = db.DB.QueryRow("SELECT COUNT(*) FROM sda.userinfo WHERE id=$1", userID).Scan(&numRows)
	assert.NoError(suite.T(), err, "could not verify db count", err)
	assert.Equal(suite.T(), 1, numRows, "there should be exactly one row in userinfo")
	var dbgroups []string
	groups = append(groups, "appleGroup", "bananaGroup")
	name = "newName"
	err = db.UpdateUserInfo(userID, name, email, groups)
	assert.NoError(suite.T(), err, "could not insert updated user info: %v", err)
	err = db.DB.QueryRow("SELECT groups FROM sda.userinfo WHERE id=$1", userID).Scan(pq.Array(&dbgroups))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), groups, dbgroups)

	db.Close()
}

func (suite *DatabaseTests) TestGetReVerificationData() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile(nil, "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		suite.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/archive/TestGetReVerificationData.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987, fmt.Sprintf("%x", sha256.New())}
	if err = db.SetArchived(fileInfo, fileID); err != nil {
		suite.FailNow("failed to archive file")
	}
	if err = db.SetVerified(fileInfo, fileID); err != nil {
		suite.FailNow("failed to mark file as verified")
	}
	accession := "acession-001"
	if err = db.SetAccessionID(accession, fileID); err != nil {
		suite.FailNow("failed to set accession id for file")
	}

	data, err := db.GetReVerificationData(accession)
	assert.NoError(suite.T(), err, "failed to get verification data")
	assert.Equal(suite.T(), "/archive/TestGetReVerificationData.c4gh", data.ArchivePath)

	db.Close()
}

func (suite *DatabaseTests) TestGetReVerificationDataFromFileID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile(nil, "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		suite.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/archive/TestGetReVerificationData.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987, fmt.Sprintf("%x", sha256.New())}
	if err = db.SetArchived(fileInfo, fileID); err != nil {
		suite.FailNow("failed to archive file")
	}
	if err = db.SetVerified(fileInfo, fileID); err != nil {
		suite.FailNow("failed to mark file as verified")
	}

	data, err := db.GetReVerificationDataFromFileID(fileID)
	assert.NoError(suite.T(), err, "failed to get verification data from fileID")
	assert.Equal(suite.T(), "/archive/TestGetReVerificationData.c4gh", data.ArchivePath)

	db.Close()
}

func (suite *DatabaseTests) TestGetReVerificationData_wrongAccessionID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile(nil, "/testuser/TestGetReVerificationData.c4gh", "testuser")
	if err != nil {
		suite.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/archive/TestGetReVerificationData.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987, fmt.Sprintf("%x", sha256.New())}

	if err = db.SetArchived(fileInfo, fileID); err != nil {
		suite.FailNow("failed to archive file")
	}
	if err = db.SetVerified(fileInfo, fileID); err != nil {
		suite.FailNow("failed to mark file as verified")
	}
	accession := "acession-001"
	if err = db.SetAccessionID(accession, fileID); err != nil {
		suite.FailNow("failed to set accession id for file")
	}

	data, err := db.GetReVerificationData("accession")
	assert.EqualError(suite.T(), err, "sql: no rows in result set")
	assert.Equal(suite.T(), schema.IngestionVerification{}, data)

	db.Close()
}

func (suite *DatabaseTests) TestGetDecryptedChecksum() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile(nil, "/testuser/TestGetDecryptedChecksum.c4gh", "testuser")
	if err != nil {
		suite.FailNow("failed to register file in database")
	}

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	if err != nil {
		suite.FailNow("failed to generate checksum")
	}

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/archive/TestGetDecryptedChecksum.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987, fmt.Sprintf("%x", sha256.New())}

	if err = db.SetArchived(fileInfo, fileID); err != nil {
		suite.FailNow("failed to archive file")
	}
	if err = db.SetVerified(fileInfo, fileID); err != nil {
		suite.FailNow("failed to mark file as verified")
	}

	checksum, err := db.GetDecryptedChecksum(fileID)
	assert.NoError(suite.T(), err, "failed to get verification data")
	assert.Equal(suite.T(), fmt.Sprintf("%x", decSha.Sum(nil)), checksum)

	db.Close()
}

func (suite *DatabaseTests) TestGetDsatasetFiles() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 3

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetDsatasetFiles-00%d.c4gh", "User-Q", i)
		fileID, err := db.RegisterFile(nil, filePath, "User-Q")
		if err != nil {
			suite.FailNow("Failed to register file")
		}
		err = db.UpdateFileEventLog(fileID, "uploaded", "User-Q", "{}", "{}")
		if err != nil {
			suite.FailNow("Failed to update file event log")
		}

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		fileInfo := FileInfo{
			fmt.Sprintf("%x", sha256.New().Sum(nil)),
			1234,
			filePath,
			checksum,
			999,
			fmt.Sprintf("%x", sha256.New()),
		}
		err = db.SetArchived(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		err = db.SetVerified(fileInfo, fileID)
		if err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "User-Q", i)
		err = db.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	dID := "test-get-dataset-files-01"
	if err := db.MapFilesToDataset(dID, []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	accessions, err := db.GetDatasetFiles(dID)
	assert.NoError(suite.T(), err, "failed to get accessions for a dataset")
	assert.Equal(suite.T(), []string{"accession_User-Q_00", "accession_User-Q_01", "accession_User-Q_02"}, accessions)

	db.Close()
}

func (suite *DatabaseTests) TestGetInboxFilePathFromID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	user := "UserX"
	filePath := fmt.Sprintf("/%v/Deletefile1.c4gh", user)
	fileID, err := db.RegisterFile(nil, filePath, user)
	if err != nil {
		suite.FailNow("Failed to register file")
	}
	err = db.UpdateFileEventLog(fileID, "uploaded", "User-z", "{}", "{}")
	if err != nil {
		suite.FailNow("Failed to update file event log")
	}
	path, err := db.getInboxFilePathFromID(user, fileID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), path, filePath)

	err = db.UpdateFileEventLog(fileID, "archived", user, "{}", "{}")
	assert.NoError(suite.T(), err)
	_, err = db.getInboxFilePathFromID(user, fileID)
	assert.Error(suite.T(), err)
	db.Close()
}

func (suite *DatabaseTests) TestGetFileIDByUserPathAndStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	user := "UserX"
	filePath := fmt.Sprintf("/%v/Deletefile1.c4gh", user)
	fileID, err := db.RegisterFile(nil, filePath, user)
	if err != nil {
		suite.FailNow("Failed to register file")
	}
	// sanity check - should fail
	_, err = db.GetFileIDByUserPathAndStatus("wrong-user", filePath, "registered")
	assert.EqualError(suite.T(), err, "sql: no rows in result set")

	// check happy path
	fileID2, err := db.getFileIDByUserPathAndStatus(user, filePath, "registered")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), fileID, fileID2)

	// update the status of the file
	err = db.UpdateFileEventLog(fileID, "archived", user, "{}", "{}")
	if err != nil {
		suite.FailNow("Failed to update file event log")
	}

	// check that an error and an empty fileID are returned when the most recent status is not registered
	fileID2, err = db.getFileIDByUserPathAndStatus(user, filePath, "registered")
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), "", fileID2)

	// check that the function works for other statuses as well
	fileID2, err = db.getFileIDByUserPathAndStatus(user, filePath, "archived")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), fileID, fileID2)

	db.Close()
}

func (suite *DatabaseTests) TestGetFileDetailsFromUUI_Found() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "failed to create new connection")

	// Register a file to get a valid UUID
	filePath := "/dummy_user.org/Dummy_folder/dummyfile.c4gh"
	user := "dummy@user.org"
	fileID, err := db.RegisterFile(nil, filePath, user)
	if err != nil {
		suite.FailNow("failed to register file in database")
	}

	// Update event log to ensure correlation ID is set
	err = db.UpdateFileEventLog(fileID, "uploaded", user, "{}", "{}")
	if err != nil {
		suite.FailNow("failed to update file event log")
	}

	infoFile, err := db.GetFileDetailsFromUUID(fileID, "uploaded")
	assert.NoError(suite.T(), err, "failed to get user and path from UUID")
	assert.Equal(suite.T(), user, infoFile.User)
	assert.Equal(suite.T(), filePath, infoFile.Path)
	db.Close()
}

func (suite *DatabaseTests) TestGetFileDetailsFromUUID_NotFound() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "failed to create new connection")

	// Use a non-existent UUID
	invalidUUID := "abc-123"
	infoFile, err := db.GetFileDetailsFromUUID(invalidUUID, "uploaded")
	assert.Error(suite.T(), err, "expected error for non-existent UUID")
	assert.Empty(suite.T(), infoFile.User)
	assert.Empty(suite.T(), infoFile.Path)

	db.Close()
}
