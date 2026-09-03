package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	ingestconf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestIngestTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

type TestSuite struct {
	suite.Suite
	ingest   Ingest
	userName string

	mockInboxReader       *mocks.MockReader
	mockArchiveReader     *mocks.MockReader
	mockArchiveWriter     *mocks.MockWriter
	mockBackupWriter      *mocks.MockWriter
	mockDB                *mocks.MockDatabase
	mockBroker            *mocks.MockBroker
	publicKey, privateKey [32]byte
}

func (ts *TestSuite) SetupSuite() {
	var err error
	ts.publicKey, ts.privateKey, err = keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("failed to create private c4gh key")
	}

	ts.ingest.ArchiveKeyList = []*[32]byte{
		&ts.privateKey,
	}

	ingestconf.SetSchemaPath("../../schemas/isolated/")

	ts.userName = "test-ingest"
}

func (ts *TestSuite) SetupTest() {
	ts.mockInboxReader = &mocks.MockReader{}
	ts.mockArchiveReader = &mocks.MockReader{}
	ts.mockArchiveWriter = &mocks.MockWriter{}
	ts.mockBackupWriter = &mocks.MockWriter{}
	ts.mockDB = &mocks.MockDatabase{}
	ts.mockBroker = &mocks.MockBroker{}

	ts.ingest.InboxReader = ts.mockInboxReader
	ts.ingest.ArchiveReader = ts.mockArchiveReader
	ts.ingest.ArchiveWriter = ts.mockArchiveWriter
	ts.ingest.BackupWriter = ts.mockBackupWriter
	ts.ingest.db = ts.mockDB
	ts.ingest.Broker = ts.mockBroker
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

func (ts *TestSuite) TestCancelFile_BaseCase() {
	fileID := uuid.NewString()
	userName := "test-cancel"
	filePath := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userName)

	ts.mockDB.On("IsFileInDataset", fileID).Return(false, nil)
	ts.mockDB.On("GetArchived", fileID).Return(&database.ArchiveData{
		FilePath:       filePath,
		Location:       "archive_unit_test_location",
		FileSize:       1,
		BackupFilePath: fileID,
		BackupLocation: "backup_unit_test_location",
	}, nil)
	ts.mockArchiveWriter.On("RemoveFile", "archive_unit_test_location", filePath).Return(nil)
	ts.mockBackupWriter.On("RemoveFile", "backup_unit_test_location", fileID).Return(nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("CancelFile", fileID, mock.Anything).Return(nil)

	message := createMessage("cancel", filePath, userName, fileID)
	_, err := ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), err, nil)
	ts.mockArchiveWriter.AssertNumberOfCalls(ts.T(), "RemoveFile", 1)
	ts.mockBackupWriter.AssertNumberOfCalls(ts.T(), "RemoveFile", 1)
	ts.mockDB.AssertNumberOfCalls(ts.T(), "CancelFile", 1)
}

func (ts *TestSuite) TestCancelFile_NotArchived() {
	fileID := uuid.NewString()
	userName := "test-cancel"
	filePath := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userName)

	ts.mockDB.On("IsFileInDataset", fileID).Return(false, nil)
	ts.mockDB.On("GetArchived", fileID).Return(nil, nil)

	message := createMessage("cancel", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err, "unexpected error when canceling file")
}

func (ts *TestSuite) TestIngestFile_BaseCase() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	encryptedContent, encryptedChecksum := ts.encryptBytes([]byte("test file content"))

	ts.mockDB.On("GetFileStatus", fileID).Return("uploaded", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("submission_unit_test_location", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockInboxReader.On("NewFileReader", "submission_unit_test_location", helper.ResolveInboxPath(filePath, userName, helper.InboxProjectConfig{})).Return(encryptedContent, nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "submitted", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockDB.On("SetKeyHash", mock.Anything, fileID).Return(nil)
	ts.mockDB.On("StoreHeader", mock.Anything, fileID).Return(nil)
	ts.mockArchiveWriter.On("WriteFile", fileID, mock.Anything).Return("archive_unit_test_location", nil)
	ts.mockArchiveReader.On("GetFileSize", "archive_unit_test_location", fileID).Return(int64(1), nil)
	ts.mockDB.On("SetArchived", "archive_unit_test_location", mock.Anything, fileID).Return(nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "archived", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")
	ts.mockArchiveWriter.AssertNumberOfCalls(ts.T(), "WriteFile", 1)
	ts.mockDB.AssertCalled(ts.T(), "SetArchived", "archive_unit_test_location", database.FileInfo{
		Size:             1,
		Path:             fileID,
		UploadedChecksum: fmt.Sprintf("%x", encryptedChecksum),
	}, fileID)
	ts.mockDB.AssertNumberOfCalls(ts.T(), "UpdateFileEventLog", 2)
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Publish", 1)
}

// Non-s3inbox flow (e.g. FEGA-Norway via TSD): nothing pre-registers the file, so ingest's
// catch-all (status "") is the first registration point. It must register AND archive in one
// pass; an early return after RegisterFile would park the file at "registered" and verify would
// never run. Guards the v3.1.72 catch-all regression.
func (ts *TestSuite) TestIngestFile_NotRegistered_FallsThroughToArchive() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	encryptedContent, encryptedChecksum := ts.encryptBytes([]byte("test file content"))

	ts.mockDB.On("GetFileStatus", fileID).Return("", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Commit").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockInboxReader.On("FindFile", helper.ResolveInboxPath(filePath, userName, helper.InboxProjectConfig{})).Return("submission_unit_test_location", nil)
	ts.mockDB.On("RegisterFile", &fileID, "submission_unit_test_location", filePath, userName).Return(fileID, nil)
	ts.mockInboxReader.On("NewFileReader", "submission_unit_test_location", helper.ResolveInboxPath(filePath, userName, helper.InboxProjectConfig{})).Return(encryptedContent, nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "submitted", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockDB.On("SetKeyHash", mock.Anything, fileID).Return(nil)
	ts.mockDB.On("StoreHeader", mock.Anything, fileID).Return(nil)
	ts.mockArchiveWriter.On("WriteFile", fileID, mock.Anything).Return("archive_unit_test_location", nil)
	ts.mockArchiveReader.On("GetFileSize", "archive_unit_test_location", fileID).Return(int64(1), nil)
	ts.mockDB.On("SetArchived", "archive_unit_test_location", mock.Anything, fileID).Return(nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "archived", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err, "unexpected error ingesting a non-registered file")

	ts.mockInboxReader.AssertNumberOfCalls(ts.T(), "FindFile", 1)
	ts.mockDB.AssertNumberOfCalls(ts.T(), "RegisterFile", 1)
	ts.mockArchiveWriter.AssertNumberOfCalls(ts.T(), "WriteFile", 1)
	ts.mockDB.AssertCalled(ts.T(), "SetArchived", "archive_unit_test_location", database.FileInfo{
		Size:             1,
		Path:             fileID,
		UploadedChecksum: fmt.Sprintf("%x", encryptedChecksum),
	}, fileID)
	ts.mockDB.AssertNumberOfCalls(ts.T(), "UpdateFileEventLog", 2)
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Publish", 1)
}

func (ts *TestSuite) TestIngestFile_NotRegistered_NotFoundInInbox() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	ts.mockDB.On("GetFileStatus", fileID).Return("", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockInboxReader.On("FindFile", helper.ResolveInboxPath(filePath, userName, helper.InboxProjectConfig{})).Return("", storageerrors.ErrorFileNotFoundInLocation)
	ts.mockBroker.On("Publish", "error", mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.Equal(ts.T(), nil, err)

	ts.mockBroker.AssertCalled(ts.T(), "Publish", "error", mock.Anything)
}

func (ts *TestSuite) TestIngestFile_NoSubmissionLocation() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	ts.mockDB.On("GetFileStatus", fileID).Return("uploaded", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockDB.On("UpdateFileEventLog", fileID, "error", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockBroker.On("Publish", "error", mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err)

	ts.mockDB.AssertCalled(ts.T(), "UpdateFileEventLog", fileID, "error", "ingest", mock.Anything, mock.Anything)
	ts.mockBroker.AssertCalled(ts.T(), "Publish", "error", mock.Anything)
}

func (ts *TestSuite) TestIngestFile_AlreadyIngested() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	ts.mockDB.On("GetFileStatus", fileID).Return("verified", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockBroker.On("Publish", "error", mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err)
	ts.mockBroker.AssertCalled(ts.T(), "Publish", "error", mock.Anything)
}

func (ts *TestSuite) TestIngestFile_MissingFile() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	ts.mockDB.On("GetFileStatus", fileID).Return("uploaded", nil)
	ts.mockDB.On("GetSubmissionLocation", fileID).Return("submission_unit_test_location", nil)
	ts.mockDB.On("BeginTransaction").Return(nil)
	ts.mockDB.On("Rollback").Return(nil)
	ts.mockInboxReader.On("NewFileReader", "submission_unit_test_location", helper.ResolveInboxPath(filePath, userName, helper.InboxProjectConfig{})).Return(nil, storageerrors.ErrorFileNotFoundInLocation)
	ts.mockDB.On("UpdateFileEventLog", fileID, "error", "ingest", mock.Anything, mock.Anything).Return(nil)
	ts.mockBroker.On("Publish", "error", mock.Anything).Return(nil)

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.NoError(ts.T(), err, "unexpected error ingesting a missing file")

	ts.mockDB.AssertCalled(ts.T(), "UpdateFileEventLog", fileID, "error", "ingest", mock.Anything, mock.Anything)
	ts.mockBroker.AssertCalled(ts.T(), "Publish", "error", mock.Anything)
}

func (ts *TestSuite) TestIngestFile_DatabaseError() {
	fileID := uuid.NewString()
	userName := "test-ingest"
	filePath := fmt.Sprintf("/%v/TestIngestMessage.c4gh", userName)

	ts.mockDB.On("GetFileStatus", fileID).Return("", errors.New("some error"))

	message := createMessage("ingest", filePath, userName, fileID)
	callbacks, err := ts.ingest.handleMessage(context.Background(), message)
	for _, cb := range callbacks {
		cb()
	}
	assert.Error(ts.T(), err, "expected error when ingest has db issue")
}

func createMessage(triggerType, filePath, userID, messageKey string) *broker.Message {
	body := schema.IngestionTrigger{
		Type:     triggerType,
		FilePath: filePath,
		User:     userID,
	}
	bodyJSON, _ := json.Marshal(body)

	return &broker.Message{Key: messageKey, Body: bodyJSON}
}
