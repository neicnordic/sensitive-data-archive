package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var archivePublicKey, archivePrivateKey, archiveKeyError = keys.GenerateKeyPair()
var syncPublicKey, syncPrivateKey, syncKeyError = keys.GenerateKeyPair()

func TestSync(t *testing.T) {
	if archiveKeyError != nil {
		t.Fatalf("archive key generation failed: %v", archiveKeyError)
	}

	if syncKeyError != nil {
		t.Fatalf("sync key generation failed: %v", syncKeyError)
	}

	for _, tc := range []struct {
		name                       string
		sourceMessage              schema.DatasetMapping
		newMocks                   func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer)
		remoteUser, remotePassword string
		withRemote                 bool
		expectedErrorContains      string
	}{
		{
			name: "mapping_success_with_remote",
			sourceMessage: schema.DatasetMapping{
				Type:         "mapping",
				DatasetID:    "test_dataset_123",
				AccessionIDs: []string{"accession_1", "accession_2", "accession_3"},
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mr := &mocks.MockReader{}
				mw := &mocks.MockWriter{}
				mdb := &mocks.MockDatabase{}
				ms := &mockServer{}

				expectedSyncDataset := schema.SyncDataset{
					DatasetID: "test_dataset_123",
					User:      "test_user",
				}
				for i, accession := range []string{"accession_1", "accession_2", "accession_3"} {
					fileContent := fmt.Sprintf("file %v content: %s", i, uuid.NewString())
					ftd, err := generateFileTestData([]byte(fileContent))
					if err != nil {
						t.Fatalf("failed to generate test file data: %v", err)
					}

					archivePath := fmt.Sprintf("archive_path_%d", i)
					submissionPath := fmt.Sprintf("/inbox_path/file_%d", i)

					mdb.On("GetInboxPath", accession).Return(submissionPath, nil).Once()
					mdb.On("GetArchivePathAndLocation", accession).Return(archivePath, "archive_location", nil).Once()

					mr.On("GetFileSize", "archive_location", archivePath).Return(int64(len(ftd.encryptedContentNoHeader)), nil).Once()
					mr.On("NewFileReader", "archive_location", archivePath).Return(ftd.encryptedContentNoHeader, nil).Once()

					mdb.On("GetHeaderByAccessionID", accession).Return(ftd.header, nil).Once()

					mw.On("WriteFile", submissionPath, mock.MatchedBy(func(content []byte) bool {
						return verifyCanDecryptAndMatch(t, content, fileContent)
					})).Return("sync_location", nil).Once()

					mdb.On("GetSyncData", accession).Return(&database.SyncData{
						User:     "test_user",
						FilePath: submissionPath,
						Checksum: ftd.unencryptedSha256Checksum,
					}, nil).Once()

					expectedSyncDataset.DatasetFiles = append(expectedSyncDataset.DatasetFiles, schema.DatasetFiles{
						FilePath: submissionPath,
						FileID:   accession,
						ShaSum:   ftd.unencryptedSha256Checksum,
					})
				}

				expectedSyncDatasetJSON, err := json.Marshal(expectedSyncDataset)
				if err != nil {
					t.Fatalf("failed to marshal expected sync data: %v", err)
				}

				ms.On("ServeHTTP", "/", expectedSyncDatasetJSON).Return(http.StatusOK).Once()

				return mr, mw, mdb, &mocks.MockBroker{}, ms
			},
			remoteUser:     "user",
			remotePassword: "password",
			withRemote:     true,
		}, {
			name: "mapping_remote_failure",
			sourceMessage: schema.DatasetMapping{
				Type:         "mapping",
				DatasetID:    "test_dataset_123",
				AccessionIDs: []string{"accession_1", "accession_2", "accession_3"},
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mr := &mocks.MockReader{}
				mw := &mocks.MockWriter{}
				mdb := &mocks.MockDatabase{}
				ms := &mockServer{}
				mb := &mocks.MockBroker{}

				expectedSyncDataset := schema.SyncDataset{
					DatasetID: "test_dataset_123",
					User:      "test_user",
				}
				for i, accession := range []string{"accession_1", "accession_2", "accession_3"} {
					fileContent := fmt.Sprintf("file %v content: %s", i, uuid.NewString())
					ftd, err := generateFileTestData([]byte(fileContent))
					if err != nil {
						t.Fatalf("failed to generate test file data: %v", err)
					}

					archivePath := fmt.Sprintf("archive_path_%d", i)
					submissionPath := fmt.Sprintf("/inbox_path/file_%d", i)

					mdb.On("GetInboxPath", accession).Return(submissionPath, nil).Once()
					mdb.On("GetArchivePathAndLocation", accession).Return(archivePath, "archive_location", nil).Once()

					mr.On("GetFileSize", "archive_location", archivePath).Return(int64(len(ftd.encryptedContentNoHeader)), nil).Once()
					mr.On("NewFileReader", "archive_location", archivePath).Return(ftd.encryptedContentNoHeader, nil).Once()

					mdb.On("GetHeaderByAccessionID", accession).Return(ftd.header, nil).Once()

					mw.On("WriteFile", submissionPath, mock.MatchedBy(func(content []byte) bool {
						return verifyCanDecryptAndMatch(t, content, fileContent)
					})).Return("sync_location", nil).Once()

					mdb.On("GetSyncData", accession).Return(&database.SyncData{
						User:     "test_user",
						FilePath: submissionPath,
						Checksum: ftd.unencryptedSha256Checksum,
					}, nil).Once()

					expectedSyncDataset.DatasetFiles = append(expectedSyncDataset.DatasetFiles, schema.DatasetFiles{
						FilePath: submissionPath,
						FileID:   accession,
						ShaSum:   ftd.unencryptedSha256Checksum,
					})
				}

				expectedSyncDatasetJSON, err := json.Marshal(expectedSyncDataset)
				if err != nil {
					t.Fatalf("failed to marshal expected sync data: %v", err)
				}

				ms.On("ServeHTTP", "/", expectedSyncDatasetJSON).Return(http.StatusInternalServerError).Once()

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					return msg.Headers != nil && msg.Headers["error-queue-reason"] == "failed to send http sync notification: 500 Internal Server Error"
				})).Return(nil).Once()

				return mr, mw, mdb, mb, ms
			},
			remoteUser:            "user",
			remotePassword:        "password",
			withRemote:            true,
			expectedErrorContains: "",
		}, {
			name: "mapping_success_no_remote",
			sourceMessage: schema.DatasetMapping{
				Type:         "mapping",
				DatasetID:    "test_dataset_123",
				AccessionIDs: []string{"accession_1", "accession_2", "accession_3"},
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mr := &mocks.MockReader{}
				mw := &mocks.MockWriter{}
				mdb := &mocks.MockDatabase{}

				for i, accession := range []string{"accession_1", "accession_2", "accession_3"} {
					fileContent := fmt.Sprintf("file %v content: %s", i, uuid.NewString())
					ftd, err := generateFileTestData([]byte(fileContent))
					if err != nil {
						t.Fatalf("failed to generate test file data: %v", err)
					}

					archivePath := fmt.Sprintf("archive_path_%d", i)
					submissionPath := fmt.Sprintf("/inbox_path/file_%d", i)

					mdb.On("GetInboxPath", accession).Return(submissionPath, nil).Once()
					mdb.On("GetArchivePathAndLocation", accession).Return(archivePath, "archive_location", nil).Once()

					mr.On("GetFileSize", "archive_location", archivePath).Return(int64(len(ftd.encryptedContentNoHeader)), nil).Once()
					mr.On("NewFileReader", "archive_location", archivePath).Return(ftd.encryptedContentNoHeader, nil).Once()

					mdb.On("GetHeaderByAccessionID", accession).Return(ftd.header, nil).Once()

					mw.On("WriteFile", submissionPath, mock.MatchedBy(func(content []byte) bool {
						return verifyCanDecryptAndMatch(t, content, fileContent)
					})).Return("sync_location", nil).Once()
				}

				return mr, mw, mdb, &mocks.MockBroker{}, &mockServer{}
			},
		}, {
			name: "mapping_failure_incorrect_file_size",
			sourceMessage: schema.DatasetMapping{
				Type:         "mapping",
				DatasetID:    "test_dataset_123",
				AccessionIDs: []string{"accession_1", "accession_2", "accession_3"},
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mr := &mocks.MockReader{}
				mdb := &mocks.MockDatabase{}

				fileContent := fmt.Sprintf("file 1 content: %s", uuid.NewString())
				ftd, err := generateFileTestData([]byte(fileContent))
				if err != nil {
					t.Fatalf("failed to generate test file data: %v", err)
				}

				archivePath := "archive_path_%1"
				submissionPath := "/inbox_path/file_1"

				mdb.On("GetInboxPath", "accession_1").Return(submissionPath, nil).Once()
				mdb.On("GetArchivePathAndLocation", "accession_1").Return(archivePath, "archive_location", nil).Once()

				mr.On("GetFileSize", "archive_location", archivePath).Return(int64(1), nil).Once()
				mr.On("NewFileReader", "archive_location", archivePath).Return(ftd.encryptedContentNoHeader, nil).Once()

				mdb.On("GetHeaderByAccessionID", "accession_1").Return(ftd.header, nil).Once()

				return mr, &mocks.MockWriter{}, mdb, &mocks.MockBroker{}, &mockServer{}
			},
			withRemote:            false,
			expectedErrorContains: "copied size does not match file size",
		}, {
			name: "dataset_release_message",
			sourceMessage: schema.DatasetMapping{
				Type:      "release",
				DatasetID: "test_dataset_123",
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				return &mocks.MockReader{}, &mocks.MockWriter{}, &mocks.MockDatabase{}, &mocks.MockBroker{}, &mockServer{}
			},
			withRemote:            false,
			expectedErrorContains: "",
		}, {
			name: "mapping_failure_inbox_path_not_found",
			sourceMessage: schema.DatasetMapping{
				Type:         "mapping",
				DatasetID:    "test_dataset_123",
				AccessionIDs: []string{"accession_1", "accession_2", "accession_3"},
			},
			newMocks: func(t *testing.T) (*mocks.MockReader, *mocks.MockWriter, *mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				mdb.On("GetInboxPath", "accession_1").Return("", sql.ErrNoRows).Once()

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					return msg.Headers != nil && msg.Headers["error-queue-reason"] == "could not sync file accession_1: failed to get inbox path, reason: sql: no rows in result set"
				})).Return(nil).Once()

				return &mocks.MockReader{}, &mocks.MockWriter{}, mdb, mb, &mockServer{}
			},
			withRemote:            false,
			expectedErrorContains: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockArchiveReader, mockSyncWriter, mockDatabase, mockBroker, mockRemoteServer := tc.newMocks(t)

			v := &sync{
				archiveC4ghPrivateKey: &archivePrivateKey,
				syncC4ghPubKey:        &syncPublicKey,
				db:                    mockDatabase,
				broker:                mockBroker,
				archiveReader:         mockArchiveReader,
				syncWriter:            mockSyncWriter,
				schemaPath:            "../../schemas/isolated/",
				syncDatasetWithPrefix: "test",
				remoteURL:             "", // Set the httptest server url if test case has withRemote
				remoteUser:            tc.remoteUser,
				remotePassword:        tc.remotePassword,
			}
			if tc.withRemote {
				mockRemote := httptest.NewServer(http.HandlerFunc(mockRemoteServer.ServeHTTP))
				defer mockRemote.Close()
				v.remoteURL = mockRemote.URL
			}

			jsonMsg, err := json.Marshal(tc.sourceMessage)
			if err != nil {
				t.Errorf("failed to marshal source message: %s", err.Error())
			}
			callbacks, err := v.handleMessage(context.Background(), &brokerv2.Message{Body: jsonMsg})
			for _, cb := range callbacks {
				cb()
			}
			if tc.expectedErrorContains == "" {
				assert.Nil(t, err)
			} else {
				assert.ErrorContains(t, err, tc.expectedErrorContains)
			}
			mockArchiveReader.AssertExpectations(t)
			mockSyncWriter.AssertExpectations(t)
			mockRemoteServer.AssertExpectations(t)
			mockDatabase.AssertExpectations(t)
			mockBroker.AssertExpectations(t)
		})
	}
}

func verifyCanDecryptAndMatch(t *testing.T, encrypted []byte, expected string) bool {
	decryptedContentReader, err := streaming.NewCrypt4GHReader(bytes.NewBuffer(encrypted), syncPrivateKey, nil)
	if err != nil {
		t.Logf("failed to decrypt content: %v", err)

		return false
	}
	defer func() {
		_ = decryptedContentReader
	}()

	decryptedContent, err := io.ReadAll(decryptedContentReader)
	if err != nil {
		t.Logf("failed to read decrypted content: %v", err)

		return false
	}
	if string(decryptedContent) != expected {
		return false
	}

	return true
}

type fileTestData struct {
	header, encryptedContentNoHeader, unencryptedContent []byte
	unencryptedMd5Checksum, unencryptedSha256Checksum    string
	encryptedContentNoHeaderSha256Checksum               string
}

func generateFileTestData(in []byte) (fileTestData, error) {
	contentBuf := &bytes.Buffer{}

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(contentBuf, archivePrivateKey, [][32]byte{archivePublicKey}, nil)
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

type mockServer struct {
	mock.Mock
}

func (s *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	expectedBody, err := io.ReadAll(r.Body)
	if err != nil {
		s.Called("failure to read request body")
	}

	args := s.Called(r.URL.Path, expectedBody)
	w.WriteHeader(args.Int(0))
}
