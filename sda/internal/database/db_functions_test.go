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

// TestMarkFileAsUploaded tests that MarkFileAsUploaded() behaves as intended
func (suite *DatabaseTests) TestUpdateFileEventLog() {
	// create database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/file2.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	// Attempt to mark a file that doesn't exist as uploaded
	err = db.UpdateFileEventLog("00000000-0000-0000-0000-000000000000", "uploaded", "testuser", "{}")
	assert.NotNil(suite.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = db.UpdateFileEventLog(fileID, "uploaded", "testuser", "{}")
	assert.NoError(suite.T(), err, "failed to set file as uploaded in database")

	exists := false
	// check that there is an "uploaded" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='uploaded')", fileID).Scan(&exists)
	assert.NoError(suite.T(), err, "Failed to check if uploaded file event exists")
	assert.True(suite.T(), exists, "UpdateFileEventLog() did not insert a row into sda.file_event_log with id: "+fileID)
}

func (suite *DatabaseTests) TestGetFileID() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	fileID, err := db.RegisterFile("/testuser/file3.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	err = db.UpdateFileStatus(fileID, "uploaded", corrID, "testuser", "{}")
	assert.NoError(suite.T(), err, "failed to update file status")

	fID, err := db.GetFileID(corrID)
	assert.NoError(suite.T(), err, "GetFileId failed")
	assert.Equal(suite.T(), fileID, fID)
}

func (suite *DatabaseTests) TestUpdateFileStatus() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/file4.c4gh", "testuser")
	assert.Nil(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	// Attempt to mark a file that doesn't exist as uploaded
	err = db.UpdateFileStatus("00000000-0000-0000-0000-000000000000", "uploaded", corrID, "testuser", "{}")
	assert.NotNil(suite.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = db.UpdateFileStatus(fileID, "uploaded", corrID, "testuser", "{}")
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
	err = db.UpdateFileStatus(fileID, "downloaded", corrID, "testuser", "{}")
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

func (suite *DatabaseTests) TestMarkCompleted() {
	db, err := NewSDAdb(suite.dbConf)
	assert.NoError(suite.T(), err, "got (%v) when creating new connection", err)

	// register a file in the database
	fileID, err := db.RegisterFile("/testuser/TestMarkCompleted.c4gh", "testuser")
	assert.NoError(suite.T(), err, "failed to register file in database")

	corrID := uuid.New().String()
	fileInfo := FileInfo{fmt.Sprintf("%x", sha256.New()), 1000, "/testuser/TestMarkCompleted.c4gh", fmt.Sprintf("%x", sha256.New()), 948}
	err = db.MarkCompleted(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as completed", err)
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
	err = db.MarkCompleted(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as completed", err)

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
	err = db.MarkCompleted(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as completed", err)
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
	err = db.MarkCompleted(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as completed", err)
	stableID := "TEST:111-1234-4567"
	err = db.SetAccessionID(stableID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	ok, err := db.CheckAccessionIDExists(stableID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	assert.True(suite.T(), ok, "CheckAccessionIDExists returned false when true was expected")
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
