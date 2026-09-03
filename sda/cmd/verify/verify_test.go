package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestVerify(t *testing.T) {
	for _, tc := range []testCase{
		verifySuccessCase,
		verifyDbErrorCase,
		verifyStorageRetryableErrorCase,
		verifyStorageNonRetryableErrorCase,
		alreadyVerifiedCase,
		reverifyCase,
		reverifyFailedArchivedChecksumCase,
		reverifyFailedDecryptedChecksumCase,
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockReader, mockDatabase, mockBroker := tc.newMocks(t)
			v := &verify{
				db:            mockDatabase,
				broker:        mockBroker,
				archiveReader: mockReader,
				archiveKeyList: []*[32]byte{
					&privateKey,
				},
				schemaPath: "../../schemas/isolated/",
				routingKey: "unit-test_destination",
			}

			jsonMsg, err := json.Marshal(tc.sourceMessage)
			if err != nil {
				t.Errorf("failed to marshal source message: %s", err.Error())
			}
			callbacks, err := v.handleMessage(context.Background(), &brokerv2.Message{Key: tc.sourceMessage.FileID, Body: jsonMsg})
			for _, cb := range callbacks {
				cb()
			}
			assert.Equal(t, tc.expectedError, err)
			tc.assertMocks(t, mockReader, mockDatabase, mockBroker)
		})
	}
}

type testCase struct {
	name          string
	sourceMessage schema.IngestionVerification
	newMocks      func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker)
	assertMocks   func(*testing.T, *mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker)
	expectedError error
}

var publicKey, privateKey, _ = keys.GenerateKeyPair()

type fileTestData struct {
	header, encryptedContentNoHeader, unencryptedContent []byte
	unencryptedMd5Checksum, unencryptedSha256Checksum    string
	encryptedContentNoHeaderSha256Checksum               string
}

func generateFileTestData(in []byte) (fileTestData, error) {
	contentBuf := &bytes.Buffer{}

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(contentBuf, privateKey, [][32]byte{publicKey}, nil)
	if err != nil {
		return fileTestData{}, fmt.Errorf("failed to init new c4gh writer: %s", err.Error())
	}
	defer func() {
		_ = crypt4GHWriter.Close()
	}()
	if _, err := io.Copy(crypt4GHWriter, bytes.NewReader(in)); err != nil {
		return fileTestData{}, fmt.Errorf("failed to write to c4gh writer: %s", err.Error())
	}
	_ = crypt4GHWriter.Close()

	unencryptedMd5Checksum := md5.New()
	unencryptedSha256Checksum := sha256.New()
	_, _ = unencryptedSha256Checksum.Write(in)
	_, _ = unencryptedMd5Checksum.Write(in)

	header, err := headers.ReadHeader(contentBuf)
	if err != nil {
		return fileTestData{}, fmt.Errorf("failed to parse crypt4gh header: %s", err.Error())
	}
	encryptedContentNoHeader, err := io.ReadAll(contentBuf)
	if err != nil {
		return fileTestData{}, fmt.Errorf("failed to read to c4gh no header content: %s", err.Error())
	}
	encryptedContentNoHeaderSha256Checksum := sha256.New()
	_, _ = encryptedContentNoHeaderSha256Checksum.Write(encryptedContentNoHeader)

	return fileTestData{
		header:                                 header,
		encryptedContentNoHeader:               encryptedContentNoHeader,
		unencryptedContent:                     in,
		unencryptedMd5Checksum:                 fmt.Sprintf("%x", unencryptedMd5Checksum.Sum(nil)),
		unencryptedSha256Checksum:              fmt.Sprintf("%x", unencryptedSha256Checksum.Sum(nil)),
		encryptedContentNoHeaderSha256Checksum: fmt.Sprintf("%x", encryptedContentNoHeaderSha256Checksum.Sum(nil)),
	}, nil
}

var verifySuccessCase = testCase{
	name: "success",
	sourceMessage: schema.IngestionVerification{
		User:               "unit_test_user",
		FilePath:           "/unit_test_file.c4gh",
		FileID:             "123",
		ArchivePath:        "/123",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           false,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		fileTestData, err := generateFileTestData([]byte("file content" + uuid.NewString()))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}

		mockDatabase.On("GetFileStatus", "123").Return("archived", nil).Once()
		mockDatabase.On("GetHeader", "123").Return(fileTestData.header, nil).Once()
		mockDatabase.On("GetArchiveLocation", "123").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/123").Return(int64(len(fileTestData.encryptedContentNoHeader)), nil).Once()
		mockReader.On("NewFileReader", "archive_location", "/123").Return(fileTestData.encryptedContentNoHeader, nil).Once()

		mockDatabase.On("GetFileInfo", "123").Return(&database.FileInfo{
			Size:              int64(len(fileTestData.encryptedContentNoHeader)),
			Path:              "/123",
			ArchivedChecksum:  "",
			DecryptedChecksum: "",
			DecryptedSize:     0,
		}, nil).Once()

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()
		mockDatabase.On("SetVerified", database.FileInfo{
			Size:              int64(len(fileTestData.encryptedContentNoHeader)),
			Path:              "",
			ArchivedChecksum:  fileTestData.encryptedContentNoHeaderSha256Checksum,
			DecryptedChecksum: fileTestData.unencryptedSha256Checksum,
			DecryptedSize:     int64(len(fileTestData.unencryptedContent)),
		}, "123").Return(nil).Once()
		mockDatabase.On("UpdateFileEventLog", "123", "verified", "verify", "{}", mock.Anything).Return(nil).Once()

		expectedMessage := schema.IngestionAccessionRequest{
			User:     "unit_test_user",
			FilePath: "/unit_test_file.c4gh",
			DecryptedChecksums: []schema.Checksums{
				{Type: "sha256", Value: fileTestData.unencryptedSha256Checksum},
				{Type: "md5", Value: fileTestData.unencryptedMd5Checksum},
			},
		}

		expectedRaw, err := json.Marshal(&expectedMessage)
		if err != nil {
			t.Errorf("failed to marshal expected message: %s", err.Error())
			t.FailNow()
		}

		mockBroker.On("Publish", "unit-test_destination", brokerv2.Message{
			Key:     "123",
			Headers: nil,
			Body:    expectedRaw,
		}).Return(nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}
var verifyDbErrorCase = testCase{
	name: "db_error",
	sourceMessage: schema.IngestionVerification{
		User:               "db_error_unit_test_user",
		FilePath:           "/db_error_unit_test_file.c4gh",
		FileID:             "db_error",
		ArchivePath:        "/db_error",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           false,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}
		mockDatabase.On("GetFileStatus", "db_error").Return("", errors.New("retryable db error")).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: errors.New("retryable db error"),
}
var verifyStorageRetryableErrorCase = testCase{
	name: "storage_error_retryable",
	sourceMessage: schema.IngestionVerification{
		User:               "storage_error_unit_test_user",
		FilePath:           "/storage_error_unit_test_file.c4gh",
		FileID:             "storage_error_retryable",
		ArchivePath:        "/storage_error_retryable",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           false,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}
		mockDatabase.On("GetFileStatus", "storage_error_retryable").Return("archived", nil).Once()
		mockDatabase.On("GetHeader", "storage_error_retryable").Return([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), nil).Once()
		mockDatabase.On("GetArchiveLocation", "storage_error_retryable").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/storage_error_retryable").Return(int64(0), errors.New("retryable storage error")).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: errors.New("retryable storage error"),
}

var verifyStorageNonRetryableErrorCase = testCase{
	name: "storage_error_not_retryable",
	sourceMessage: schema.IngestionVerification{
		User:               "storage_error_unit_test_user",
		FilePath:           "/storage_error_unit_test_file.c4gh",
		FileID:             "storage_error_not_retryable",
		ArchivePath:        "/storage_error_not_retryable",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           false,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}
		mockDatabase.On("GetFileStatus", "storage_error_not_retryable").Return("archived", nil).Once()
		mockDatabase.On("GetHeader", "storage_error_not_retryable").Return([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), nil).Once()
		mockDatabase.On("GetArchiveLocation", "storage_error_not_retryable").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/storage_error_not_retryable").Return(int64(0), storageerrors.ErrorFileNotFoundInLocation).Once()

		mockDatabase.On("UpdateFileEventLog", "storage_error_not_retryable", "error", "verify", `{"error":"file not found in archive storage"}`, mock.Anything).Return(nil).Once()

		expectedMessage := schema.IngestionVerification{
			User:               "storage_error_unit_test_user",
			FilePath:           "/storage_error_unit_test_file.c4gh",
			FileID:             "storage_error_not_retryable",
			ArchivePath:        "/storage_error_not_retryable",
			EncryptedChecksums: []schema.Checksums{},
			ReVerify:           false,
		}

		expectedRaw, err := json.Marshal(&expectedMessage)
		if err != nil {
			t.Errorf("failed to marshal expected message: %s", err.Error())
			t.FailNow()
		}

		mockBroker.On("Publish", "error", brokerv2.Message{
			Key: "storage_error_not_retryable",
			Headers: map[string]any{
				"error-queue-reason": "file not found in archive storage",
			},
			Body: expectedRaw,
		}).Return(nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var alreadyVerifiedCase = testCase{
	name: "alreadyVerified",
	sourceMessage: schema.IngestionVerification{
		User:               "unit_test_user",
		FilePath:           "/unit_test_file_321.c4gh",
		FileID:             "321",
		ArchivePath:        "/321",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           false,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		fileTestData, err := generateFileTestData([]byte("file content" + uuid.NewString()))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
		mockDatabase.On("GetFileStatus", "321").Return("verified", nil).Once()
		mockDatabase.On("GetHeader", "321").Return(fileTestData.header, nil).Once()
		mockDatabase.On("GetArchiveLocation", "321").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/321").Return(int64(len(fileTestData.encryptedContentNoHeader)), nil).Once()
		mockReader.On("NewFileReader", "archive_location", "/321").Return(fileTestData.encryptedContentNoHeader, nil).Once()

		mockDatabase.On("GetFileInfo", "321").Return(&database.FileInfo{
			Size:              int64(len(fileTestData.encryptedContentNoHeader)),
			ArchivedChecksum:  fileTestData.encryptedContentNoHeaderSha256Checksum,
			DecryptedChecksum: fileTestData.unencryptedSha256Checksum,
			DecryptedSize:     int64(len(fileTestData.unencryptedContent)),
		}, nil).Once()

		mockDatabase.On("BeginTransaction").Return(nil).Once()
		mockDatabase.On("Rollback").Return(nil).Once()
		mockDatabase.On("Commit").Return(nil).Once()
		mockDatabase.On("UpdateFileEventLog", "321", "verified", "verify", "{}", mock.Anything).Return(nil).Once()

		expectedMessage := schema.IngestionAccessionRequest{
			User:     "unit_test_user",
			FilePath: "/unit_test_file_321.c4gh",
			DecryptedChecksums: []schema.Checksums{
				{Type: "sha256", Value: fileTestData.unencryptedSha256Checksum},
				{Type: "md5", Value: fileTestData.unencryptedMd5Checksum},
			},
		}

		expectedRaw, err := json.Marshal(&expectedMessage)
		if err != nil {
			t.Errorf("failed to marshal expected message: %s", err.Error())
			t.FailNow()
		}

		mockBroker.On("Publish", "unit-test_destination", brokerv2.Message{
			Key:     "321",
			Headers: nil,
			Body:    expectedRaw,
		}).Return(nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var reverifyTestData, _ = generateFileTestData([]byte("file content" + uuid.NewString()))
var reverifyCase = testCase{
	name: "reverify",
	sourceMessage: schema.IngestionVerification{
		User:        "unit_test_user",
		FilePath:    "/unit_test_file.c4gh",
		FileID:      "111",
		ArchivePath: "/111",
		EncryptedChecksums: []schema.Checksums{
			{Type: "sha256", Value: reverifyTestData.encryptedContentNoHeaderSha256Checksum},
		},
		ReVerify: true,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		mockDatabase.On("GetFileStatus", "111").Return("ready", nil).Once()
		mockDatabase.On("GetHeader", "111").Return(reverifyTestData.header, nil).Once()
		mockDatabase.On("GetArchiveLocation", "111").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/111").Return(int64(len(reverifyTestData.encryptedContentNoHeader)), nil).Once()
		mockReader.On("NewFileReader", "archive_location", "/111").Return(reverifyTestData.encryptedContentNoHeader, nil).Once()

		mockDatabase.On("GetDecryptedChecksum", "111").Return(reverifyTestData.unencryptedSha256Checksum, nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var reverifyFailedArchivedChecksumCase = testCase{
	name: "reverify_failed_archived_checksum",
	sourceMessage: schema.IngestionVerification{
		User:        "unit_test_user",
		FilePath:    "/unit_test_file.c4gh",
		FileID:      "112",
		ArchivePath: "/112",
		EncryptedChecksums: []schema.Checksums{
			{Type: "sha256", Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		ReVerify: true,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		fileTestData, err := generateFileTestData([]byte("file content" + uuid.NewString()))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}

		mockDatabase.On("GetFileStatus", "112").Return("ready", nil).Once()
		mockDatabase.On("GetHeader", "112").Return(fileTestData.header, nil).Once()
		mockDatabase.On("GetArchiveLocation", "112").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/112").Return(int64(len(fileTestData.encryptedContentNoHeader)), nil).Once()
		mockReader.On("NewFileReader", "archive_location", "/112").Return(fileTestData.encryptedContentNoHeader, nil).Once()

		mockDatabase.On("GetDecryptedChecksum", "112").Return(fileTestData.unencryptedSha256Checksum, nil).Once()

		mockDatabase.On("UpdateFileEventLog", "112", "error", "verify", `{"error":"archived checksum don't match"}`, mock.Anything).Return(nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}

var reverifyFailedDecryptedChecksumCase = testCase{
	name: "reverify_failed_decrypted_checksum",
	sourceMessage: schema.IngestionVerification{
		User:               "unit_test_user",
		FilePath:           "/unit_test_file.c4gh",
		FileID:             "112",
		ArchivePath:        "/112",
		EncryptedChecksums: []schema.Checksums{},
		ReVerify:           true,
	},
	newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockDatabase, *mocks.MockBroker) {
		mockReader := &mocks.MockReader{}
		mockDatabase := &mocks.MockDatabase{}
		mockBroker := &mocks.MockBroker{}

		fileTestData, err := generateFileTestData([]byte("file content" + uuid.NewString()))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}

		mockDatabase.On("GetFileStatus", "112").Return("ready", nil).Once()
		mockDatabase.On("GetHeader", "112").Return(fileTestData.header, nil).Once()
		mockDatabase.On("GetArchiveLocation", "112").Return("archive_location", nil).Once()

		mockReader.On("GetFileSize", "archive_location", "/112").Return(int64(len(fileTestData.encryptedContentNoHeader)), nil).Once()
		mockReader.On("NewFileReader", "archive_location", "/112").Return(fileTestData.encryptedContentNoHeader, nil).Once()

		mockDatabase.On("GetDecryptedChecksum", "112").Return("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil).Once()

		mockDatabase.On("UpdateFileEventLog", "112", "error", "verify", `{"error":"decrypted checksum don't match"}`, mock.Anything).Return(nil).Once()

		return mockReader, mockDatabase, mockBroker
	},
	assertMocks: func(t *testing.T, mockReader *mocks.MockReader, mockDatabase *mocks.MockDatabase, mockBroker *mocks.MockBroker) {
		mockReader.AssertExpectations(t)
		mockDatabase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	},
	expectedError: nil,
}
