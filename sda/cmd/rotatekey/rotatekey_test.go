package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

var oldPublicKey, _, oldKeyGenerationError = keys.GenerateKeyPair()
var targetPublicKey, _, targetKeyGenerationError = keys.GenerateKeyPair()

type mockReencryptClient struct {
	mock.Mock
}

func (mrc *mockReencryptClient) ReencryptHeader(ctx context.Context, in *reencrypt.ReencryptRequest, _ ...grpc.CallOption) (*reencrypt.ReencryptResponse, error) {
	args := mrc.Called(in.GetPublickey(), in.GetOldheader())

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rsp := args.Get(0)
	if rsp == nil {
		return nil, args.Error(1)
	}

	return rsp.(*reencrypt.ReencryptResponse), args.Error(1)
}

func TestRotateKey(t *testing.T) {
	if oldKeyGenerationError != nil {
		t.Fatalf("old key generation error: %v", oldKeyGenerationError)
	}
	if targetKeyGenerationError != nil {
		t.Fatalf("target key generation error: %v", targetKeyGenerationError)
	}

	for _, tc := range []struct {
		name                            string
		sourceMessage                   schema.KeyRotation
		reencryptClientTimeout          time.Duration
		newMocks                        func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient)
		expectedError                   error
		expectedTargetKeyNotUsableError error
	}{
		{
			name: "success",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				mrc := &mockReencryptClient{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()
				oldKeyHash := hex.EncodeToString(oldPublicKey[:])
				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(oldKeyHash, nil).Once()
				mdb.On("GetHeader", "00000000-0000-0000-0000-000000000000").Return([]byte("old_header"), nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()
				mdb.On("Rollback").Return(nil).Once()
				mdb.On("Commit").Return(nil).Once()
				mdb.On("BackupHeader", "00000000-0000-0000-0000-000000000000", []byte("old_header"), oldKeyHash).Return(nil).Once()

				mrc.On("ReencryptHeader", targetPublicKeyPemEncoded, []byte("old_header")).Return(&reencrypt.ReencryptResponse{
					Header: []byte("new_header"),
				}, nil).Once()

				mdb.On("RotateHeaderKey", []byte("new_header"), newKeyHash, "00000000-0000-0000-0000-000000000000").Return(nil).Once()

				mdb.On("GetReVerificationDataFromFileID", "00000000-0000-0000-0000-000000000000").Return(&database.ReVerificationData{
					FileID:               "00000000-0000-0000-0000-000000000000",
					ArchiveFilePath:      "/archive/test_file",
					SubmissionFilePath:   "/inbox/test_file",
					SubmissionUser:       "test_user",
					ArchivedCheckSum:     "1234123412341234123412341234123412341234123412341234123412341234",
					ArchivedCheckSumType: "sha256",
				}, nil).Once()

				mb.On("Publish", "reverify_queue", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var reverifyMessage schema.IngestionVerification

					if err := json.Unmarshal(msg.Body, &reverifyMessage); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return reverifyMessage.User == "test_user" &&
						reverifyMessage.FilePath == "/inbox/test_file" &&
						reverifyMessage.FileID == "00000000-0000-0000-0000-000000000000" &&
						reverifyMessage.ArchivePath == "/archive/test_file" &&
						len(reverifyMessage.EncryptedChecksums) == 1 &&
						reverifyMessage.EncryptedChecksums[0].Type == "sha256" &&
						reverifyMessage.EncryptedChecksums[0].Value == "1234123412341234123412341234123412341234123412341234123412341234" &&
						reverifyMessage.ReVerify
				})).Return(nil).Once()

				return mdb, mb, mrc
			},
		}, {
			name: "publish_failure",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				mrc := &mockReencryptClient{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()
				oldKeyHash := hex.EncodeToString(oldPublicKey[:])
				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(oldKeyHash, nil).Once()
				mdb.On("GetHeader", "00000000-0000-0000-0000-000000000000").Return([]byte("old_header"), nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()
				mdb.On("Rollback").Return(nil).Once()
				mdb.On("Commit").Return(nil).Once()
				mdb.On("BackupHeader", "00000000-0000-0000-0000-000000000000", []byte("old_header"), oldKeyHash).Return(nil).Once()

				mrc.On("ReencryptHeader", targetPublicKeyPemEncoded, []byte("old_header")).Return(&reencrypt.ReencryptResponse{
					Header: []byte("new_header"),
				}, nil).Once()

				mdb.On("RotateHeaderKey", []byte("new_header"), newKeyHash, "00000000-0000-0000-0000-000000000000").Return(nil).Once()

				mdb.On("GetReVerificationDataFromFileID", "00000000-0000-0000-0000-000000000000").Return(&database.ReVerificationData{
					FileID:               "00000000-0000-0000-0000-000000000000",
					ArchiveFilePath:      "/archive/test_file",
					SubmissionFilePath:   "/inbox/test_file",
					SubmissionUser:       "test_user",
					ArchivedCheckSum:     "1234123412341234123412341234123412341234123412341234123412341234",
					ArchivedCheckSumType: "sha256",
				}, nil).Once()

				mb.On("Publish", "reverify_queue", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var reverifyMessage schema.IngestionVerification

					if err := json.Unmarshal(msg.Body, &reverifyMessage); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return reverifyMessage.User == "test_user" &&
						reverifyMessage.FilePath == "/inbox/test_file" &&
						reverifyMessage.FileID == "00000000-0000-0000-0000-000000000000" &&
						reverifyMessage.ArchivePath == "/archive/test_file" &&
						len(reverifyMessage.EncryptedChecksums) == 1 &&
						reverifyMessage.EncryptedChecksums[0].Type == "sha256" &&
						reverifyMessage.EncryptedChecksums[0].Value == "1234123412341234123412341234123412341234123412341234123412341234" &&
						reverifyMessage.ReVerify
				})).Return(errors.New("publish failure")).Once()

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var keyRotationMsg schema.KeyRotation

					if err := json.Unmarshal(msg.Body, &keyRotationMsg); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return keyRotationMsg.Type == "key_rotation" &&
						keyRotationMsg.FileID == "00000000-0000-0000-0000-000000000000" &&
						msg.Headers != nil && msg.Headers["error-queue-reason"] == "failed to publish re verify message after database transaction committed"
				})).Return(nil).Once()

				return mdb, mb, mrc
			},
		}, {
			name: "reencrypt_retryable_error",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				mrc := &mockReencryptClient{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()
				oldKeyHash := hex.EncodeToString(oldPublicKey[:])
				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(oldKeyHash, nil).Once()
				mdb.On("GetHeader", "00000000-0000-0000-0000-000000000000").Return([]byte("old_header"), nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()
				mdb.On("Rollback").Return(nil).Once()
				mdb.On("BackupHeader", "00000000-0000-0000-0000-000000000000", []byte("old_header"), oldKeyHash).Return(nil).Once()

				mrc.On("ReencryptHeader", targetPublicKeyPemEncoded, []byte("old_header")).Return(nil, errors.New("error")).Once()

				return mdb, mb, mrc
			},
			expectedError: errors.New("error"),
		}, {
			name: "reencrypt_timeout",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				mrc := &mockReencryptClient{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()
				oldKeyHash := hex.EncodeToString(oldPublicKey[:])
				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(oldKeyHash, nil).Once()
				mdb.On("GetHeader", "00000000-0000-0000-0000-000000000000").Return([]byte("old_header"), nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()
				mdb.On("Rollback").Return(nil).Once()
				mdb.On("BackupHeader", "00000000-0000-0000-0000-000000000000", []byte("old_header"), oldKeyHash).Return(nil).Once()

				mrc.On("ReencryptHeader", targetPublicKeyPemEncoded, []byte("old_header")).Run(func(_ mock.Arguments) {
					time.Sleep(1 * time.Second)
				}).Return(&reencrypt.ReencryptResponse{
					Header: []byte("not expected to succeed"),
				}, nil).Once()

				return mdb, mb, mrc
			},
			reencryptClientTimeout: 500 * time.Millisecond,
			expectedError:          context.DeadlineExceeded,
		}, {
			name: "retryable_db_error",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}

				mdb.On("ListKeyHashes").Return(nil, errors.New("retryable error")).Once()

				return mdb, &mocks.MockBroker{}, &mockReencryptClient{}
			},
			expectedError: errors.New("retryable error"),
		}, {
			name: "file_already_target_key",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(targetPublicKeyPemEncoded string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()

				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(newKeyHash, nil).Once()

				return mdb, &mocks.MockBroker{}, &mockReencryptClient{}
			},
		}, {
			name: "file_key_hash_not_found",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(_ string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()

				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return("", sql.ErrNoRows).Once()

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var keyRotationMsg schema.KeyRotation

					if err := json.Unmarshal(msg.Body, &keyRotationMsg); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return keyRotationMsg.Type == "key_rotation" &&
						keyRotationMsg.FileID == "00000000-0000-0000-0000-000000000000" &&
						msg.Headers != nil && msg.Headers["error-queue-reason"] == "file key hash not found"
				})).Return(nil).Once()

				return mdb, mb, &mockReencryptClient{}
			},
		}, {
			name: "file_key_header_not_found",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(_ string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: "",
				}}, nil).Once()

				oldKeyHash := hex.EncodeToString(oldPublicKey[:])
				mdb.On("GetKeyHash", "00000000-0000-0000-0000-000000000000").Return(oldKeyHash, nil).Once()
				mdb.On("GetHeader", "00000000-0000-0000-0000-000000000000").Return(nil, sql.ErrNoRows).Once()

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var keyRotationMsg schema.KeyRotation

					if err := json.Unmarshal(msg.Body, &keyRotationMsg); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return keyRotationMsg.Type == "key_rotation" &&
						keyRotationMsg.FileID == "00000000-0000-0000-0000-000000000000" &&
						msg.Headers != nil && msg.Headers["error-queue-reason"] == "file header not found"
				})).Return(nil).Once()

				return mdb, mb, &mockReencryptClient{}
			},
		}, {
			name: "incoming_message_not_valid",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "",
			},
			newMocks: func(_ string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mb := &mocks.MockBroker{}

				mb.On("Publish", "error", mock.MatchedBy(func(msg brokerv2.Message) bool {
					var keyRotationMsg schema.KeyRotation

					if err := json.Unmarshal(msg.Body, &keyRotationMsg); err != nil {
						log.Errorf("failed to unmarshal mock message: %v", err)

						return false
					}

					return keyRotationMsg.Type == "key_rotation" &&
						keyRotationMsg.FileID == "" &&
						msg.Headers != nil && msg.Headers["error-queue-reason"] == "validation of incoming message failed"
				})).Return(nil).Once()

				return &mocks.MockDatabase{}, mb, &mockReencryptClient{}
			},
		}, {
			name: "target_key_not_deprecated",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(_ string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				newKeyHash := hex.EncodeToString(targetPublicKey[:])
				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{
					Hash:         newKeyHash,
					DeprecatedAt: time.Now().Format(time.RFC3339),
				}}, nil).Once()

				return mdb, mb, &mockReencryptClient{}
			},
			expectedError:                   ErrorKeyDeprecated,
			expectedTargetKeyNotUsableError: ErrorKeyDeprecated,
		}, {
			name: "target_key_not_registered",
			sourceMessage: schema.KeyRotation{
				Type:   "key_rotation",
				FileID: "00000000-0000-0000-0000-000000000000",
			},
			newMocks: func(_ string) (*mocks.MockDatabase, *mocks.MockBroker, *mockReencryptClient) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}

				mdb.On("ListKeyHashes").Return([]*database.C4ghKeyHash{}, nil).Once()

				return mdb, mb, &mockReencryptClient{}
			},
			expectedError:                   ErrorKeyNotRegistered,
			expectedTargetKeyNotUsableError: ErrorKeyNotRegistered,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := &bytes.Buffer{}
			if err := keys.WriteCrypt4GHX25519PublicKey(tmp, targetPublicKey); err != nil {
				t.Fatal(fmt.Errorf("failed to encode public key to pem: %w", err))
			}
			targetPublicKeyPemEncoded := base64.StdEncoding.EncodeToString(tmp.Bytes())

			mockDatabase, mockBroker, mockReencrypt := tc.newMocks(targetPublicKeyPemEncoded)

			rk := &rotateKey{
				broker:                    mockBroker,
				db:                        mockDatabase,
				reverifyRoutingKey:        "reverify_queue",
				targetPublicKeyPemEncoded: targetPublicKeyPemEncoded,
				schemaPath:                "../../schemas/isolated/",
				targetPublicKey:           &targetPublicKey,
				targetKeyNotUsableChan:    make(chan error, 1),
				reencryptClient:           mockReencrypt,
				reencryptClientTimeout:    max(tc.reencryptClientTimeout, 100*time.Millisecond),
			}

			jsonMsg, err := json.Marshal(tc.sourceMessage)
			if err != nil {
				t.Errorf("failed to marshal source message: %s", err.Error())
			}
			callbacks, err := rk.handleMessage(context.Background(), &brokerv2.Message{Key: tc.sourceMessage.FileID, Body: jsonMsg})
			for _, cb := range callbacks {
				cb()
			}
			assert.Equal(t, tc.expectedError, err)
			if tc.expectedTargetKeyNotUsableError != nil {
				select {
				case err := <-rk.targetKeyNotUsableChan:
					assert.Equal(t, tc.expectedTargetKeyNotUsableError, err)
				case <-time.After(2 * time.Second):
					t.Error("timed out waiting for target key not usable error")
					t.Fail()
				}
			}

			mockDatabase.AssertExpectations(t)
			mockBroker.AssertExpectations(t)
			mockReencrypt.AssertExpectations(t)
		})
	}
}
