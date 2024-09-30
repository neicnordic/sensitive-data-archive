package database

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestRegisterFile tests that RegisterFile() behaves as intended
func (suite *DatabaseTests) TestRegisterFile() {

	// create database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/file1.c4gh", "testuser")
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

}

func (suite *DatabaseTests) TestGetFileID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	fileID, err := db.RegisterFile("/testuser/file3.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	err = db.UpdateFileEventLog(fileID, "uploaded", corrID, "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update file status")

	fID, err := db.GetFileID(corrID)
	assert.NoError(suite.T(), err, "GetFileId failed")
	assert.Equal(suite.T(), fileID, fID)
}

func (suite *DatabaseTests) TestUpdateFileEventLog() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/file4.c4gh", "testuser")
	assert.Nil(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	// Attempt to mark a file that doesn't exist as uploaded
	err = db.UpdateFileEventLog("00000000-0000-0000-0000-000000000000", "uploaded", corrID, "testuser", "{}", "{}")
	assert.NotNil(suite.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = db.UpdateFileEventLog(fileID, "uploaded", corrID, "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to set file as uploaded in database")

	exists := false
	// check that there is an "uploaded" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='uploaded')", fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "Failed to check if uploaded file event exists")
	assert.True(suite.T(), exists, "UpdateFileEventLog() did not insert a row into sda.file_event_log with id: "+fileID)
}

func (suite *DatabaseTests) TestStoreHeader() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestStoreHeader.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	// store header for non existing entry
	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, "00000000-0000-0000-0000-000000000000")
	assert.EqualError(suite.T(), err, "something went wrong with the query zero rows were changed")
}

func (suite *DatabaseTests) TestSetArchived() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestSetArchived.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetArchived.c4gh", fmt.Sprintf("%x", sha256.New()), -1}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetArchived(fileInfo, "00000000-0000-0000-0000-000000000000", corrID)
	assert.ErrorContains(suite.T(), err, "violates foreign key constraint")

	err = db.SetArchived(fileInfo, fileID, "00000000-0000-0000-0000-000000000000")
	assert.ErrorContains(suite.T(), err, "duplicate key value violates unique constraint")
}

func (suite *DatabaseTests) TestGetFileStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetFileStatus.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	err = db.UpdateFileEventLog(fileID, "downloaded", corrID, "testuser", "{}", "{}")
	assert.NoError(suite.T(), err, "failed to set file as downloaded in database")

	status, err := db.GetFileStatus(corrID)
	assert.NoError(suite.T(), err, "failed to get file status")
	assert.Equal(suite.T(), "downloaded", status)
}

func (suite *DatabaseTests) TestGetHeader() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetHeader.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte{15, 45, 20, 40, 48}, fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	header, err := db.GetHeader(fileID)
	assert.NoError(suite.T(), err, "failed to get file header")
	assert.Equal(suite.T(), []byte{15, 45, 20, 40, 48}, header)
}

func (suite *DatabaseTests) TestSetVerified() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestSetVerified.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/testuser/TestSetVerified.c4gh", fmt.Sprintf("%x", sha256.New()), 948}
	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
}

func (suite *DatabaseTests) TestGetArchived() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetArchived.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestGetArchived.c4gh", fmt.Sprintf("%x", sha256.New()), 987}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	filePath, fileSize, err := db.GetArchived(fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), 1000, fileSize)
	assert.Equal(suite.T(), "/tmp/TestGetArchived.c4gh", filePath)
}

func (suite *DatabaseTests) TestSetAccessionID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestSetAccessionID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestSetAccessionID.c4gh", fmt.Sprintf("%x", sha256.New()), 987}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)
	stableID := "TEST:000-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
}

func (suite *DatabaseTests) TestCheckAccessionIDExists() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestCheckAccessionIDExists.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/tmp/TestCheckAccessionIDExists.c4gh", fmt.Sprintf("%x", sha256.New()), 987}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID, corrID)
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
}

func (suite *DatabaseTests) TestGetFileInfo() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetFileInfo.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := FileInfo{fmt.Sprintf("%x", encSha.Sum(nil)), 2000, "/tmp/TestGetFileInfo.c4gh", fmt.Sprintf("%x", decSha.Sum(nil)), 1987}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as Archived")
	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	info, err := db.GetFileInfo(fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.Equal(suite.T(), int64(2000), info.Size)
	assert.Equal(suite.T(), "/tmp/TestGetFileInfo.c4gh", info.Path)
	assert.Equal(suite.T(), "11c94bc7fb13afeb2b3fb16c1dbe9206dc09560f1b31420f2d46210ca4ded0a8", info.Checksum)
	assert.Equal(suite.T(), "a671218c2418aa51adf97e33c5c91a720289ba3c9fd0d36f6f4bf9610730749f", info.DecryptedChecksum)
}

func (suite *DatabaseTests) TestMapFilesToDataset() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 1; i < 12; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/testuser/TestMapFilesToDataset-%d.c4gh", i), "testuser")
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
}

func (suite *DatabaseTests) TestGetInboxPath() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
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
}

func (suite *DatabaseTests) TestUpdateDatasetEvent() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	accessions := []string{}
	for i := 0; i < 5; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/testuser/TestGetInboxPath-00%d.c4gh", i), "testuser")
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
}

func (suite *DatabaseTests) TestGetHeaderForStableID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetHeaderForStableID.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	err = db.StoreHeader([]byte("HEADER"), fileID)
	assert.NoError(suite.T(), err, "failed to store file header")

	stableID := "TEST:010-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when setting stable ID: %s, %s", err, stableID, fileID)

	header, err := db.GetHeaderForStableID("TEST:010-1234-4567")
	assert.NoError(suite.T(), err, "failed to get header for stable ID: %v", err)
	assert.Equal(suite.T(), header, []byte("HEADER"), "did not get expected header")
}

func (suite *DatabaseTests) TestGetSyncData() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestGetGetSyncData.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, "/tmp/TestGetGetSyncData.c4gh", checksum, 999}
	corrID := uuid.New().String()
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Verified")

	stableID := "TEST:000-1111-2222"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when setting stable ID: %s, %s", err, stableID, fileID)

	fileData, err := db.getSyncData("TEST:000-1111-2222")
	assert.NoError(suite.T(), err, "failed to get sync data for file")
	assert.Equal(suite.T(), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", fileData.Checksum, "did not get expected file checksum")
	assert.Equal(suite.T(), "/testuser/TestGetGetSyncData.c4gh", fileData.FilePath, "did not get expected file path")
	assert.Equal(suite.T(), "testuser", fileData.User, "did not get expected user")
}

func (suite *DatabaseTests) TestCheckIfDatasetExists() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	accessions := []string{}
	for i := 0; i <= 3; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/testuser/TestCheckIfDatasetExists-%d.c4gh", i), "testuser")
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
}

func (suite *DatabaseTests) TestGetArchivePath() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	fileID, err := db.RegisterFile("/testuser/TestGetArchivePath-001.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	checksum := fmt.Sprintf("%x", sha256.New())
	corrID := uuid.New().String()
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1234, corrID, checksum, 999}
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetAccessionID("acession-0001", fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	path, err := db.getArchivePath("acession-0001")
	assert.NoError(suite.T(), err, "getArchivePath failed")
	assert.Equal(suite.T(), path, corrID)
}

func (suite *DatabaseTests) TestGetUserFiles() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5
	testUser := "GetFilesUser"

	for i := 0; i < testCases; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", testUser, i), testUser)
		assert.NoError(suite.T(), err, "failed to register file in database")
		err = db.UpdateFileEventLog(fileID, "uploaded", fileID, testUser, "{}", "{}")
		assert.NoError(suite.T(), err, "failed to update satus of file in database")
		err = db.UpdateFileEventLog(fileID, "ready", fileID, testUser, "{}", "{}")
		assert.NoError(suite.T(), err, "failed to update satus of file in database")
	}
	filelist, err := db.GetUserFiles("unknownuser")
	assert.NoError(suite.T(), err, "failed to get (empty) file list of unknown user")
	assert.Empty(suite.T(), filelist, "file list of unknown user is not empty")

	filelist, err = db.GetUserFiles(testUser)
	assert.NoError(suite.T(), err, "failed to get file list")
	assert.Equal(suite.T(), testCases, len(filelist), "file list is of incorrect length")

	for _, fileInfo := range filelist {
		assert.Equal(suite.T(), "ready", fileInfo.Status, "incorrect file status")
	}
}

func (suite *DatabaseTests) TestGetCorrID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	filePath := "/testuser/file10.c4gh"
	user := "testuser"

	fileID, err := db.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = db.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	corrID, err := db.GetCorrID(user, filePath)
	assert.NoError(suite.T(), err, "failed to get correlation ID of file in database")
	assert.Equal(suite.T(), fileID, corrID)

	checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, "/testuser/file10.c4gh", checksum, 999}
	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Verified")

	stableID := "TEST:get-corr-id"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when setting stable ID: %s, %s", err, stableID, fileID)

	diSet := map[string][]string{
		"dataset-corr-id": {"TEST:get-corr-id"},
	}

	for di, acs := range diSet {
		err := db.MapFilesToDataset(di, acs)
		assert.NoError(suite.T(), err, "failed to map file to dataset")
	}

	corrID2, err := db.GetCorrID(user, filePath)
	assert.Error(suite.T(), err, "failed to get correlation ID of file in database")
	assert.Equal(suite.T(), "", corrID2)
}

func (suite *DatabaseTests) TestListActiveUsers() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5
	testUsers := []string{"User-A", "User-B", "User-C", "User-D"}

	for _, user := range testUsers {
		for i := 0; i < testCases; i++ {
			filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i)
			fileID, err := db.RegisterFile(filePath, user)
			if err != nil {
				suite.FailNow("Failed to register file")
			}
			err = db.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("Failed to update file event log")
			}

			corrID, err := db.GetCorrID(user, filePath)
			if err != nil {
				suite.FailNow("Failed to get CorrID for file")
			}
			assert.Equal(suite.T(), fileID, corrID)

			checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
			fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New().Sum(nil)), 1234, filePath, checksum, 999}
			err = db.SetArchived(fileInfo, fileID, corrID)
			if err != nil {
				suite.FailNow("failed to mark file as Archived")
			}

			err = db.SetVerified(fileInfo, fileID, corrID)
			if err != nil {
				suite.FailNow("failed to mark file as Verified")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = db.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
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
}

func (suite *DatabaseTests) TestGetDatasetStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)
	testCases := 5

	for i := 0; i < testCases; i++ {
		filePath := fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", "User-Q", i)
		fileID, err := db.RegisterFile(filePath, "User-Q")
		if err != nil {
			suite.FailNow("Failed to register file")
		}
		err = db.UpdateFileEventLog(fileID, "uploaded", fileID, "User-Q", "{}", "{}")
		if err != nil {
			suite.FailNow("Failed to update file event log")
		}

		corrID, err := db.GetCorrID("User-Q", filePath)
		if err != nil {
			suite.FailNow("Failed to get CorrID for file")
		}
		assert.Equal(suite.T(), fileID, corrID)

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		fileInfo := FileInfo{
			fmt.Sprintf("%x", sha256.New().Sum(nil)),
			1234,
			filePath,
			checksum,
			999,
		}
		err = db.SetArchived(fileInfo, fileID, corrID)
		if err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		err = db.SetVerified(fileInfo, fileID, corrID)
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
}

func (suite *DatabaseTests) TestAddKey() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// Test registering a new key and its description
	keyHex := `2d2d2d2d2d424547494e204352595054344748205055424c4943204b4559
2d2d2d2d2d0a65574d394166785761626d775354627657346e736650646b
432f6953412b3849712b6e516232555a2b6d6f3d0a2d2d2d2d2d454e4420
4352595054344748205055424c4943204b45592d2d2d2d2d0a`
	keyDescription := "this is a test key"
	err = db.addKeyHash(keyHex, keyDescription)
	assert.NoError(suite.T(), err, "failed to register key in database")

	// Verify that the key was added
	var exists bool
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.encryption_keys WHERE key_hash=$1 AND description=$2)", keyHex, keyDescription).
		Scan(&exists)
	assert.NoError(suite.T(), err, "failed to verify key hash existence")
	assert.True(suite.T(), exists, "key hash was not added to the database")
}
