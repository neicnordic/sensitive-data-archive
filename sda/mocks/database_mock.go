package mocks

import (
	"context"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/stretchr/testify/mock"
)

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) UpdateUserInfo(_ context.Context, userID, name, email string, groups []string) error {
	args := m.Called(userID, name, email, groups)

	return args.Error(0)
}

func (m *MockDatabase) SetKeyHash(_ context.Context, keyHash, fileID string) error {
	args := m.Called(keyHash, fileID)

	return args.Error(0)
}

func (m *MockDatabase) SetBackedUp(_ context.Context, location, path, fileID string) error {
	args := m.Called(location, path, fileID)

	return args.Error(0)
}

func (m *MockDatabase) ListUserDatasets(_ context.Context, submissionUser string) ([]*database.DatasetInfo, error) {
	args := m.Called(submissionUser)

	return args.Get(0).([]*database.DatasetInfo), args.Error(1)
}

func (m *MockDatabase) ListKeyHashes(_ context.Context) ([]*database.C4ghKeyHash, error) {
	args := m.Called()

	return args.Get(0).([]*database.C4ghKeyHash), args.Error(1)
}

func (m *MockDatabase) ListDatasets(_ context.Context) ([]*database.DatasetInfo, error) {
	args := m.Called()

	return args.Get(0).([]*database.DatasetInfo), args.Error(1)
}

func (m *MockDatabase) ListActiveUsers(_ context.Context) ([]string, error) {
	args := m.Called()

	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDatabase) GetSizeAndObjectCountOfLocation(_ context.Context, location string) (uint64, uint64, error) {
	args := m.Called(location)

	return args.Get(0).(uint64), args.Get(1).(uint64), args.Error(2)
}

func (m *MockDatabase) GetReVerificationData(_ context.Context, accessionID string) (*database.ReVerificationData, error) {
	args := m.Called(accessionID)

	return args.Get(0).(*database.ReVerificationData), args.Error(1)
}

func (m *MockDatabase) GetReVerificationDataFromFileID(_ context.Context, fileID string) (*database.ReVerificationData, error) {
	args := m.Called(fileID)

	return args.Get(0).(*database.ReVerificationData), args.Error(1)
}
func (m *MockDatabase) GetKeyHash(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetFileDetails(_ context.Context, fileID, event string) (*database.FileDetails, error) {
	args := m.Called(fileID, event)

	return args.Get(0).(*database.FileDetails), args.Error(1)
}

func (m *MockDatabase) GetFileStatusHistory(ctx context.Context, fileID string) ([]database.FileStatus, error) {
	args := m.Called(fileID)

	return args.Get(0).([]database.FileStatus), args.Error(1)
}

func (m *MockDatabase) GetFileEvents(ctx context.Context) ([]string, error) {
	args := m.Called()

	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDatabase) GetDecryptedChecksum(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetDatasetStatus(_ context.Context, datasetID string) (string, error) {
	args := m.Called(datasetID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetDatasetFileIDs(_ context.Context, datasetID string) ([]string, error) {
	args := m.Called(datasetID)

	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDatabase) GetDatasetFiles(_ context.Context, datasetID string) ([]string, error) {
	args := m.Called(datasetID)

	return args.Get(0).([]string), args.Error(1)
}

func (m *MockDatabase) AddKeyHash(_ context.Context, keyHash, keyDescription string) error {
	args := m.Called(keyHash, keyDescription)

	return args.Error(0)
}

func (m *MockDatabase) DeprecateKeyHash(_ context.Context, keyHash string) error {
	args := m.Called(keyHash)

	return args.Error(0)
}

func (m *MockDatabase) BeginTransaction(_ context.Context) (database.Transaction, error) {
	panic("function not expected to be called in unit tests")
}

func (m *MockDatabase) Close() error {
	panic("function not expected to be called in unit tests")
}

func (m *MockDatabase) SchemaVersion() (int, error) {
	args := m.Called()

	return args.Get(0).(int), args.Error(1)
}

func (m *MockDatabase) Ping(_ context.Context) error {
	args := m.Called()

	return args.Error(0)
}

func (m *MockDatabase) RegisterFile(_ context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error) {
	args := m.Called(fileID, inboxLocation, uploadPath, uploadUser)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetUploadedSubmissionFilePathAndLocation(_ context.Context, submissionUser, fileID string) (string, string, error) {
	args := m.Called(submissionUser, fileID)

	return args.Get(0).(string), args.Get(1).(string), args.Error(2)
}

func (m *MockDatabase) GetFileIDByUserPathAndStatus(_ context.Context, submissionUser, filePath, status string) (string, error) {
	args := m.Called(submissionUser, filePath, status)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) UpdateFileEventLog(_ context.Context, fileID, event, user, details, message string) error {
	args := m.Called(fileID, event, user, details, message)

	return args.Error(0)
}

func (m *MockDatabase) StoreHeader(_ context.Context, header []byte, id string) error {
	args := m.Called(header, id)

	return args.Error(0)
}

func (m *MockDatabase) RotateHeaderKey(_ context.Context, header []byte, keyHash, fileID string) error {
	args := m.Called(header, keyHash, fileID)

	return args.Error(0)
}

func (m *MockDatabase) SetArchived(_ context.Context, location string, file *database.FileInfo, fileID string) error {
	args := m.Called(location, file, fileID)

	return args.Error(0)
}

func (m *MockDatabase) CancelFile(_ context.Context, fileID, message string) error {
	args := m.Called(fileID, message)

	return args.Error(0)
}

func (m *MockDatabase) IsFileInDataset(_ context.Context, fileID string) (bool, error) {
	args := m.Called(fileID)

	return args.Get(0).(bool), args.Error(1)
}

func (m *MockDatabase) GetFileStatus(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetHeader(_ context.Context, fileID string) ([]byte, error) {
	args := m.Called(fileID)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockDatabase) BackupHeader(_ context.Context, fileID string, header []byte, keyHash string) error {
	args := m.Called(fileID, header, keyHash)

	return args.Error(0)
}

func (m *MockDatabase) SetVerified(_ context.Context, file *database.FileInfo, fileID string) error {
	args := m.Called(file, fileID)

	return args.Error(0)
}

func (m *MockDatabase) GetArchived(_ context.Context, fileID string) (*database.ArchiveData, error) {
	args := m.Called(fileID)

	return args.Get(0).(*database.ArchiveData), args.Error(1)
}

func (m *MockDatabase) CheckAccessionIDExists(_ context.Context, accessionID, fileID string) (string, error) {
	args := m.Called(accessionID, fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) SetAccessionID(_ context.Context, accessionID, fileID string) error {
	args := m.Called(accessionID, fileID)

	return args.Error(0)
}

func (m *MockDatabase) GetAccessionID(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) MapFileToDataset(_ context.Context, datasetID, fileID string) error {
	args := m.Called(datasetID, fileID)

	return args.Error(0)
}

func (m *MockDatabase) GetInboxPath(_ context.Context, accessionID string) (string, error) {
	args := m.Called(accessionID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) UpdateDatasetEvent(_ context.Context, datasetID, status, message string) error {
	args := m.Called(datasetID, status, message)

	return args.Error(0)
}

func (m *MockDatabase) GetFileInfo(_ context.Context, id string) (*database.FileInfo, error) {
	args := m.Called(id)

	return args.Get(0).(*database.FileInfo), args.Error(1)
}

func (m *MockDatabase) GetSubmissionLocation(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) GetHeaderByAccessionID(_ context.Context, accessionID string) ([]byte, error) {
	args := m.Called(accessionID)

	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockDatabase) GetMappingData(_ context.Context, accessionID string) (*database.MappingData, error) {
	args := m.Called(accessionID)

	return args.Get(0).(*database.MappingData), args.Error(1)
}

func (m *MockDatabase) GetSyncData(_ context.Context, accessionID string) (*database.SyncData, error) {
	args := m.Called(accessionID)

	return args.Get(0).(*database.SyncData), args.Error(1)
}

func (m *MockDatabase) GetFileIDInInbox(_ context.Context, submissionUser, filePath string) (string, error) {
	args := m.Called(submissionUser, filePath)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) CheckIfDatasetExists(_ context.Context, datasetID string) (bool, error) {
	args := m.Called(datasetID)

	return args.Get(0).(bool), args.Error(1)
}

func (m *MockDatabase) GetArchivePathAndLocation(_ context.Context, accessionID string) (string, string, error) {
	args := m.Called(accessionID)

	return args.Get(0).(string), args.Get(1).(string), args.Error(2)
}

func (m *MockDatabase) GetArchiveLocation(_ context.Context, fileID string) (string, error) {
	args := m.Called(fileID)

	return args.Get(0).(string), args.Error(1)
}

func (m *MockDatabase) SetSubmissionFileSize(_ context.Context, fileID string, size int64) error {
	args := m.Called(fileID, size)

	return args.Error(0)
}

func (m *MockDatabase) GetUserFiles(_ context.Context, userID, pathPrefix string, allData bool, limit int, cursor string) ([]*database.SubmissionFileInfo, string, error) {
	args := m.Called(userID, pathPrefix, allData, limit, cursor)

	return args.Get(0).([]*database.SubmissionFileInfo), args.Get(1).(string), args.Error(2)
}
