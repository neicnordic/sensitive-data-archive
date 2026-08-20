package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	appconf "github.com/neicnordic/sensitive-data-archive/cmd/finalize/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/mocks"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
	app Finalize

	mockArchiveReader *mocks.MockReader
	mockBackupWriter  *mocks.MockWriter
	mockDB            *mocks.MockDatabase
	mockBroker        *mocks.MockBroker

	publicKey, privateKey [32]byte
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) SetupSuite() {
	var err error
	ts.publicKey, ts.privateKey, err = keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("failed to create private c4gh key")
	}
	viper.Set("log.level", "debug")
	appconf.SetSchemaPath("../../schemas/isolated/")
}

func (ts *TestSuite) SetupTest() {
	ts.mockArchiveReader = &mocks.MockReader{}
	ts.mockBackupWriter = &mocks.MockWriter{}
	ts.mockDB = &mocks.MockDatabase{}
	ts.mockBroker = &mocks.MockBroker{}

	ts.app.archiveReader = ts.mockArchiveReader
	ts.app.backupWriter = ts.mockBackupWriter
	ts.app.db = ts.mockDB
	ts.app.broker = ts.mockBroker
}

func createMessage(filePath, userName, accession, messageKey string) *broker.Message {
	body := schema.IngestionAccession{
		Type:        "accession",
		FilePath:    filePath,
		User:        userName,
		AccessionID: accession,
		DecryptedChecksums: []schema.Checksums{
			{Type: "sha256", Value: "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	return &broker.Message{Key: messageKey, Body: bodyJSON}
}
func (ts *TestSuite) encryptBytes(in []byte) ([]byte, []byte) {
	contentBuf := &bytes.Buffer{}

	sha256hash := sha256.New()
	mr := io.MultiWriter(contentBuf, sha256hash)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(mr, ts.privateKey, [][32]byte{ts.publicKey}, nil)
	if err != nil {
		ts.FailNow("failed to create crypt4gh writer")
	}
	defer func() {
		_ = crypt4GHWriter.Close()
	}()
	if _, err := io.Copy(crypt4GHWriter, bytes.NewReader(in)); err != nil {
		ts.FailNow("failed to write to crypt4gh writer")
	}

	return contentBuf.Bytes(), sha256hash.Sum(nil)
}

func (ts *TestSuite) TestBackupFile() {
	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"
	message := createMessage(filePath, userName, accession, fileID)

	encryptedContent, _ := ts.encryptBytes([]byte("test file content"))
	ts.mockArchiveReader.On("NewFileReader", "archive_test_location", fileID).Return(encryptedContent, nil)
	ts.mockArchiveReader.On("GetFileSize", "archive_test_location", fileID).Return(int64(1234), nil)
	ts.mockBackupWriter.On("WriteFile", fileID).Return("backup_test_location", nil)
	ts.mockDB.On("GetArchived", fileID).Return(&database.ArchiveData{
		FilePath: fileID,
		FileSize: int64(1234),
		Location: "archive_test_location",
	}, nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("SetBackedUp", "backup_test_location", mock.Anything, fileID).Return(nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "backed up", "finalize", mock.Anything, mock.Anything).Return(nil)
	ts.mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)

	tx, _ := ts.mockDB.BeginTransaction(context.Background())

	assert.Equal(ts.T(), nil, ts.app.backupFile(context.Background(), tx, message))

	tx.Commit()
}

func (ts *TestSuite) TestHandleMessage_disabled() {
	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"

	ts.mockDB.On("GetFileStatus", fileID).Return("disabled", nil)

	message := createMessage(filePath, userName, accession, fileID)
	_, err := ts.app.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestHandleMessage_ready() {
	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"

	ts.mockDB.On("GetFileStatus", fileID).Return("ready", nil)

	message := createMessage(filePath, userName, accession, fileID)
	_, err := ts.app.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestHandleMessage_other() {
	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"

	ts.mockDB.On("GetFileStatus", fileID).Return("", nil)

	message := createMessage(filePath, userName, accession, fileID)
	_, err := ts.app.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), fmt.Sprintf("file with file-id: %s is not verified yet, aborting work", fileID), err.Error())
}

func (ts *TestSuite) TestSetAccession_duplicate() {
	ts.app.archiveReader = nil
	ts.app.backupWriter = nil

	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"
	message := createMessage(filePath, userName, accession, fileID)
	var content schema.IngestionAccession
	_ = json.Unmarshal(message.Body, &content)

	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("CheckAccessionIDExists", accession, fileID).Return("duplicate", nil)

	_, err := ts.app.setAccession(context.Background(), &content, message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestSetAccession_ok() {
	ts.app.archiveReader = nil
	ts.app.backupWriter = nil

	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"
	message := createMessage(filePath, userName, accession, fileID)
	var content schema.IngestionAccession
	_ = json.Unmarshal(message.Body, &content)

	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("CheckAccessionIDExists", accession, fileID).Return("", nil)
	ts.mockDB.On("SetAccessionID", accession, fileID).Return(nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "ready", "finalize", mock.Anything, mock.Anything).Return(nil)

	ts.mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)

	_, err := ts.app.setAccession(context.Background(), &content, message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestSetAccession_same() {
	ts.app.archiveReader = nil
	ts.app.backupWriter = nil

	fileID := uuid.NewString()
	userName := "test-finalize"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)
	accession := "file-asdfg-1234"
	message := createMessage(filePath, userName, accession, fileID)
	var content schema.IngestionAccession
	_ = json.Unmarshal(message.Body, &content)

	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("CheckAccessionIDExists", accession, fileID).Return("same", nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "ready", "finalize", mock.Anything, mock.Anything).Return(nil)

	ts.mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)

	_, err := ts.app.setAccession(context.Background(), &content, message)
	assert.Equal(ts.T(), nil, err)
}
