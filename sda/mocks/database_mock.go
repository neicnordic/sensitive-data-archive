package mocks

import (
	"context"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
)

// MockDatabase implements database.Database using plain function fields.
// In each test only set the fields for methods that will actually be called.
// Unset methods return zero values; if you need to assert a method is NOT
// called, set the field to t.Fatal (see examples in api_test.go).
type MockDatabase struct {
	BeginTransactionFunc                         func(ctx context.Context) (database.Transaction, error)
	CloseFunc                                    func() error
	SchemaVersionFunc                            func() (int, error)
	PingFunc                                     func(ctx context.Context) error
	RegisterFileFunc                             func(ctx context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error)
	GetUploadedSubmissionFilePathAndLocationFunc func(ctx context.Context, submissionUser, fileID string) (string, string, error)
	GetFileIDByUserPathAndStatusFunc             func(ctx context.Context, submissionUser, filePath, status string) (string, error)
	CheckAccessionIDOwnedByUserFunc              func(ctx context.Context, accessionID, user string) (bool, error)
	UpdateFileEventLogFunc                       func(ctx context.Context, fileID, event, user, details, message string) error
	StoreHeaderFunc                              func(ctx context.Context, header []byte, id string) error
	RotateHeaderKeyFunc                          func(ctx context.Context, header []byte, keyHash, fileID string) error
	SetArchivedFunc                              func(ctx context.Context, location string, file *database.FileInfo, fileID string) error
	CancelFileFunc                               func(ctx context.Context, fileID, message string) error
	IsFileInDatasetFunc                          func(ctx context.Context, fileID string) (bool, error)
	GetFileStatusFunc                            func(ctx context.Context, fileID string) (string, error)
	GetHeaderFunc                                func(ctx context.Context, fileID string) ([]byte, error)
	BackupHeaderFunc                             func(ctx context.Context, fileID string, header []byte, keyHash string) error
	SetVerifiedFunc                              func(ctx context.Context, file *database.FileInfo, fileID string) error
	GetArchivedFunc                              func(ctx context.Context, fileID string) (*database.ArchiveData, error)
	CheckAccessionIDExistsFunc                   func(ctx context.Context, accessionID, fileID string) (string, error)
	SetAccessionIDFunc                           func(ctx context.Context, accessionID, fileID string) error
	GetAccessionIDFunc                           func(ctx context.Context, fileID string) (string, error)
	MapFileToDatasetFunc                         func(ctx context.Context, datasetID, fileID string) error
	GetInboxPathFunc                             func(ctx context.Context, accessionID string) (string, error)
	UpdateDatasetEventFunc                       func(ctx context.Context, datasetID, status, message string) error
	GetFileInfoFunc                              func(ctx context.Context, id string) (*database.FileInfo, error)
	GetSubmissionLocationFunc                    func(ctx context.Context, fileID string) (string, error)
	GetHeaderByAccessionIDFunc                   func(ctx context.Context, accessionID string) ([]byte, error)
	GetMappingDataFunc                           func(ctx context.Context, accessionID string) (*database.MappingData, error)
	GetSyncDataFunc                              func(ctx context.Context, accessionID string) (*database.SyncData, error)
	GetFileIDInInboxFunc                         func(ctx context.Context, submissionUser, filePath string) (string, error)
	CheckIfDatasetExistsFunc                     func(ctx context.Context, datasetID string) (bool, error)
	GetArchivePathAndLocationFunc                func(ctx context.Context, accessionID string) (string, string, error)
	GetArchiveLocationFunc                       func(ctx context.Context, fileID string) (string, error)
	SetSubmissionFileSizeFunc                    func(ctx context.Context, fileID string, size int64) error
	GetUserFilesFunc                             func(ctx context.Context, userID, pathPrefix string, allData bool, limit int, cursor string) ([]*database.SubmissionFileInfo, string, error)
	AddKeyHashFunc                               func(ctx context.Context, keyHash, keyDescription string) error
	DeprecateKeyHashFunc                         func(ctx context.Context, keyHash string) error
	GetDatasetFilesFunc                          func(ctx context.Context, datasetID string) ([]string, error)
	GetDatasetFileIDsFunc                        func(ctx context.Context, datasetID string) ([]string, error)
	GetDatasetStatusFunc                         func(ctx context.Context, datasetID string) (string, error)
	GetDecryptedChecksumFunc                     func(ctx context.Context, fileID string) (string, error)
	GetReVerificationDataFunc                    func(ctx context.Context, accessionID string) (*database.ReVerificationData, error)
	GetFileDetailsFunc                           func(ctx context.Context, fileID, event string) (*database.FileDetails, error)
	GetKeyHashFunc                               func(ctx context.Context, fileID string) (string, error)
	GetReVerificationDataFromFileIDFunc          func(ctx context.Context, fileID string) (*database.ReVerificationData, error)
	GetSizeAndObjectCountOfLocationFunc          func(ctx context.Context, location string) (uint64, uint64, error)
	ListActiveUsersFunc                          func(ctx context.Context) ([]string, error)
	SetBackedUpFunc                              func(ctx context.Context, location, path, fileID string) error
	ListDatasetsFunc                             func(ctx context.Context) ([]*database.DatasetInfo, error)
	ListKeyHashesFunc                            func(ctx context.Context) ([]*database.C4ghKeyHash, error)
	ListUserDatasetsFunc                         func(ctx context.Context, submissionUser string) ([]*database.DatasetInfo, error)
	SetKeyHashFunc                               func(ctx context.Context, keyHash, fileID string) error
	UpdateUserInfoFunc                           func(ctx context.Context, userID, name, email string, groups []string) error
}

func (m *MockDatabase) UpdateUserInfo(ctx context.Context, userID, name, email string, groups []string) error {
	return nil
}

func (m *MockDatabase) SetKeyHash(ctx context.Context, keyHash, fileID string) error {
	return nil
}

func (m *MockDatabase) SetBackedUp(ctx context.Context, location, path, fileID string) error {
	return nil
}

func (m *MockDatabase) ListUserDatasets(ctx context.Context, submissionUser string) ([]*database.DatasetInfo, error) {
	return nil, nil
}

func (m *MockDatabase) ListKeyHashes(ctx context.Context) ([]*database.C4ghKeyHash, error) {
	return nil, nil
}

func (m *MockDatabase) ListDatasets(ctx context.Context) ([]*database.DatasetInfo, error) {
	return nil, nil
}

func (m *MockDatabase) ListActiveUsers(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *MockDatabase) GetSizeAndObjectCountOfLocation(ctx context.Context, location string) (uint64, uint64, error) {
	return 0, 0, nil
}

func (m *MockDatabase) GetReVerificationData(ctx context.Context, accessionID string) (*database.ReVerificationData, error) {
	return nil, nil
}

func (m *MockDatabase) GetReVerificationDataFromFileID(ctx context.Context, fileID string) (*database.ReVerificationData, error) {
	return nil, nil
}
func (m *MockDatabase) GetKeyHash(ctx context.Context, fileID string) (string, error) {
	return "", nil
}

func (m *MockDatabase) GetFileDetails(ctx context.Context, fileID, event string) (*database.FileDetails, error) {
	return nil, nil
}
func (m *MockDatabase) GetDecryptedChecksum(ctx context.Context, fileID string) (string, error) {
	return "", nil
}

func (m *MockDatabase) GetDatasetStatus(ctx context.Context, datasetID string) (string, error) {
	return "", nil
}

func (m *MockDatabase) GetDatasetFileIDs(ctx context.Context, datasetID string) ([]string, error) {
	return nil, nil
}

func (m *MockDatabase) GetDatasetFiles(ctx context.Context, datasetID string) ([]string, error) {
	return nil, nil
}

func (m *MockDatabase) AddKeyHash(ctx context.Context, keyHash, keyDescription string) error {
	return nil
}

func (m *MockDatabase) DeprecateKeyHash(ctx context.Context, keyHash string) error {
	return nil
}

func (m *MockDatabase) BeginTransaction(ctx context.Context) (database.Transaction, error) {
	if m.BeginTransactionFunc != nil {
		return m.BeginTransactionFunc(ctx)
	}
	return nil, nil
}
func (m *MockDatabase) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
func (m *MockDatabase) SchemaVersion() (int, error) {
	if m.SchemaVersionFunc != nil {
		return m.SchemaVersionFunc()
	}
	return 0, nil
}
func (m *MockDatabase) Ping(ctx context.Context) error {
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return nil
}
func (m *MockDatabase) RegisterFile(ctx context.Context, fileID *string, inboxLocation, uploadPath, uploadUser string) (string, error) {
	if m.RegisterFileFunc != nil {
		return m.RegisterFileFunc(ctx, fileID, inboxLocation, uploadPath, uploadUser)
	}
	return "", nil
}
func (m *MockDatabase) GetUploadedSubmissionFilePathAndLocation(ctx context.Context, submissionUser, fileID string) (string, string, error) {
	if m.GetUploadedSubmissionFilePathAndLocationFunc != nil {
		return m.GetUploadedSubmissionFilePathAndLocationFunc(ctx, submissionUser, fileID)
	}
	return "", "", nil
}
func (m *MockDatabase) GetFileIDByUserPathAndStatus(ctx context.Context, submissionUser, filePath, status string) (string, error) {
	if m.GetFileIDByUserPathAndStatusFunc != nil {
		return m.GetFileIDByUserPathAndStatusFunc(ctx, submissionUser, filePath, status)
	}
	return "", nil
}
func (m *MockDatabase) CheckAccessionIDOwnedByUser(ctx context.Context, accessionID, user string) (bool, error) {
	if m.CheckAccessionIDOwnedByUserFunc != nil {
		return m.CheckAccessionIDOwnedByUserFunc(ctx, accessionID, user)
	}
	return false, nil
}
func (m *MockDatabase) UpdateFileEventLog(ctx context.Context, fileID, event, user, details, message string) error {
	if m.UpdateFileEventLogFunc != nil {
		return m.UpdateFileEventLogFunc(ctx, fileID, event, user, details, message)
	}
	return nil
}
func (m *MockDatabase) StoreHeader(ctx context.Context, header []byte, id string) error {
	if m.StoreHeaderFunc != nil {
		return m.StoreHeaderFunc(ctx, header, id)
	}
	return nil
}
func (m *MockDatabase) RotateHeaderKey(ctx context.Context, header []byte, keyHash, fileID string) error {
	if m.RotateHeaderKeyFunc != nil {
		return m.RotateHeaderKeyFunc(ctx, header, keyHash, fileID)
	}
	return nil
}
func (m *MockDatabase) SetArchived(ctx context.Context, location string, file *database.FileInfo, fileID string) error {
	if m.SetArchivedFunc != nil {
		return m.SetArchivedFunc(ctx, location, file, fileID)
	}
	return nil
}
func (m *MockDatabase) CancelFile(ctx context.Context, fileID, message string) error {
	if m.CancelFileFunc != nil {
		return m.CancelFileFunc(ctx, fileID, message)
	}
	return nil
}
func (m *MockDatabase) IsFileInDataset(ctx context.Context, fileID string) (bool, error) {
	if m.IsFileInDatasetFunc != nil {
		return m.IsFileInDatasetFunc(ctx, fileID)
	}
	return false, nil
}
func (m *MockDatabase) GetFileStatus(ctx context.Context, fileID string) (string, error) {
	if m.GetFileStatusFunc != nil {
		return m.GetFileStatusFunc(ctx, fileID)
	}
	return "", nil
}
func (m *MockDatabase) GetHeader(ctx context.Context, fileID string) ([]byte, error) {
	if m.GetHeaderFunc != nil {
		return m.GetHeaderFunc(ctx, fileID)
	}
	return nil, nil
}
func (m *MockDatabase) BackupHeader(ctx context.Context, fileID string, header []byte, keyHash string) error {
	if m.BackupHeaderFunc != nil {
		return m.BackupHeaderFunc(ctx, fileID, header, keyHash)
	}
	return nil
}
func (m *MockDatabase) SetVerified(ctx context.Context, file *database.FileInfo, fileID string) error {
	if m.SetVerifiedFunc != nil {
		return m.SetVerifiedFunc(ctx, file, fileID)
	}
	return nil
}
func (m *MockDatabase) GetArchived(ctx context.Context, fileID string) (*database.ArchiveData, error) {
	if m.GetArchivedFunc != nil {
		return m.GetArchivedFunc(ctx, fileID)
	}
	return nil, nil
}
func (m *MockDatabase) CheckAccessionIDExists(ctx context.Context, accessionID, fileID string) (string, error) {
	if m.CheckAccessionIDExistsFunc != nil {
		return m.CheckAccessionIDExistsFunc(ctx, accessionID, fileID)
	}
	return "", nil
}
func (m *MockDatabase) SetAccessionID(ctx context.Context, accessionID, fileID string) error {
	if m.SetAccessionIDFunc != nil {
		return m.SetAccessionIDFunc(ctx, accessionID, fileID)
	}
	return nil
}
func (m *MockDatabase) GetAccessionID(ctx context.Context, fileID string) (string, error) {
	if m.GetAccessionIDFunc != nil {
		return m.GetAccessionIDFunc(ctx, fileID)
	}
	return "", nil
}
func (m *MockDatabase) MapFileToDataset(ctx context.Context, datasetID, fileID string) error {
	if m.MapFileToDatasetFunc != nil {
		return m.MapFileToDatasetFunc(ctx, datasetID, fileID)
	}
	return nil
}
func (m *MockDatabase) GetInboxPath(ctx context.Context, accessionID string) (string, error) {
	if m.GetInboxPathFunc != nil {
		return m.GetInboxPathFunc(ctx, accessionID)
	}
	return "", nil
}
func (m *MockDatabase) UpdateDatasetEvent(ctx context.Context, datasetID, status, message string) error {
	if m.UpdateDatasetEventFunc != nil {
		return m.UpdateDatasetEventFunc(ctx, datasetID, status, message)
	}
	return nil
}
func (m *MockDatabase) GetFileInfo(ctx context.Context, id string) (*database.FileInfo, error) {
	if m.GetFileInfoFunc != nil {
		return m.GetFileInfoFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockDatabase) GetSubmissionLocation(ctx context.Context, fileID string) (string, error) {
	if m.GetSubmissionLocationFunc != nil {
		return m.GetSubmissionLocationFunc(ctx, fileID)
	}
	return "", nil
}
func (m *MockDatabase) GetHeaderByAccessionID(ctx context.Context, accessionID string) ([]byte, error) {
	if m.GetHeaderByAccessionIDFunc != nil {
		return m.GetHeaderByAccessionIDFunc(ctx, accessionID)
	}
	return nil, nil
}
func (m *MockDatabase) GetMappingData(ctx context.Context, accessionID string) (*database.MappingData, error) {
	if m.GetMappingDataFunc != nil {
		return m.GetMappingDataFunc(ctx, accessionID)
	}
	return nil, nil
}
func (m *MockDatabase) GetSyncData(ctx context.Context, accessionID string) (*database.SyncData, error) {
	if m.GetSyncDataFunc != nil {
		return m.GetSyncDataFunc(ctx, accessionID)
	}
	return nil, nil
}
func (m *MockDatabase) GetFileIDInInbox(ctx context.Context, submissionUser, filePath string) (string, error) {
	if m.GetFileIDInInboxFunc != nil {
		return m.GetFileIDInInboxFunc(ctx, submissionUser, filePath)
	}
	return "", nil
}
func (m *MockDatabase) CheckIfDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	if m.CheckIfDatasetExistsFunc != nil {
		return m.CheckIfDatasetExistsFunc(ctx, datasetID)
	}
	return false, nil
}
func (m *MockDatabase) GetArchivePathAndLocation(ctx context.Context, accessionID string) (string, string, error) {
	if m.GetArchivePathAndLocationFunc != nil {
		return m.GetArchivePathAndLocationFunc(ctx, accessionID)
	}
	return "", "", nil
}
func (m *MockDatabase) GetArchiveLocation(ctx context.Context, fileID string) (string, error) {
	if m.GetArchiveLocationFunc != nil {
		return m.GetArchiveLocationFunc(ctx, fileID)
	}
	return "", nil
}
func (m *MockDatabase) SetSubmissionFileSize(ctx context.Context, fileID string, size int64) error {
	if m.SetSubmissionFileSizeFunc != nil {
		return m.SetSubmissionFileSizeFunc(ctx, fileID, size)
	}
	return nil
}
func (m *MockDatabase) GetUserFiles(ctx context.Context, userID, pathPrefix string, allData bool, limit int, cursor string) ([]*database.SubmissionFileInfo, string, error) {
	if m.GetUserFilesFunc != nil {
		return m.GetUserFilesFunc(ctx, userID, pathPrefix, allData, limit, cursor)
	}
	return nil, "", nil
}
