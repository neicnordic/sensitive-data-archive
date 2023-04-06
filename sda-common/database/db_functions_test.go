package database

import (
	"regexp"

	"github.com/stretchr/testify/assert"
)

// TestRegisterFile tests that RegisterFile() behaves as intended
func (suite *DatabaseTests) TestRegisterFile() {

	// create database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.Nil(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileId, err := db.RegisterFile("/testuser/file1.c4gh", "testuser")
	if db.Version < 4 {
		assert.NotNil(suite.T(), err, "RegisterFile() should not work in db version %v", db.Version)

		return
	}
	assert.Nil(suite.T(), err, "failed to register file in database")

	// check that the returning fileId is a uuid
	uuidPattern := "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	r := regexp.MustCompile(uuidPattern)
	assert.True(suite.T(), r.MatchString(fileId), "RegisterFile() did not return a valid UUID: "+fileId)

	// check that the file is in the database
	exists := false
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.files WHERE id=$1)", fileId).Scan(&exists)
	assert.Nil(suite.T(), err, "Failed to check if registered file exists")
	assert.True(suite.T(), exists, "RegisterFile() did not insert a row into sda.files with id: "+fileId)

	// check that there is a "registered" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='registered')", fileId).Scan(&exists)
	assert.Nil(suite.T(), err, "Failed to check if registered file event exists")
	assert.True(suite.T(), exists, "RegisterFile() did not insert a row into sda.file_event_log with id: "+fileId)

}

// TestMarkFileAsUploaded tests that MarkFileAsUploaded() behaves as intended
func (suite *DatabaseTests) TestMarkFileAsUploaded() {

	// create database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.Nil(suite.T(), err, "got %v when creating new connection", err)

	// register a file in the database
	fileId, err := db.RegisterFile("/testuser/file2.c4gh", "testuser")
	if db.Version < 4 {
		assert.NotNil(suite.T(), err, "MarkFileAsUploaded() should not work in db version %v", db.Version)

		return
	}
	assert.Nil(suite.T(), err, "failed to register file in database")

	// Attempt to mark a file that doesn't exist as uploaded
	err = db.MarkFileAsUploaded("00000000-0000-0000-0000-000000000000", "testuser", "{}")
	assert.NotNil(suite.T(), err, "Unknown file could be marked as uploaded in database")

	// mark file as uploaded
	err = db.MarkFileAsUploaded(fileId, "testuser", "{}")
	assert.Nil(suite.T(), err, "failed to set file as uploaded in database")

	exists := false
	// check that there is an "uploaded" file event connected to the file
	err = db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sda.file_event_log WHERE file_id=$1 AND event='uploaded')", fileId).Scan(&exists)
	assert.Nil(suite.T(), err, "Failed to check if uploaded file event exists")
	assert.True(suite.T(), exists, "MarkFileAsUploaded() did not insert a row into sda.file_event_log with id: "+fileId)
}
