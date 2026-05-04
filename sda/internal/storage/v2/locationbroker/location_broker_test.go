package locationbroker

import (
	"context"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type LocationBrokerTestSuite struct {
	suite.Suite
}

type mockDatabase struct {
	mock.Mock
}

func (m *mockDatabase) BeginTransaction(_ context.Context) (database.Transaction, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) Close() error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SchemaVersion() (int, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) Ping(_ context.Context) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) RegisterFile(_ context.Context, _x *string, _, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetFileIDByUserPathAndStatus(_ context.Context, _, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) CheckAccessionIDOwnedByUser(_ context.Context, _, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) UpdateFileEventLog(_ context.Context, _, _, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) StoreHeader(_ context.Context, _ []byte, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) RotateHeaderKey(_ context.Context, _ []byte, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetArchived(_ context.Context, _ string, _ *database.FileInfo, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetFileStatus(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetHeader(_ context.Context, _ string) ([]byte, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) BackupHeader(_ context.Context, _ string, _ []byte, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetVerified(_ context.Context, _ *database.FileInfo, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetArchived(_ context.Context, _ string) (*database.ArchiveData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) CheckAccessionIDExists(_ context.Context, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetAccessionID(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetAccessionID(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) MapFileToDataset(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetInboxPath(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) UpdateDatasetEvent(_ context.Context, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetFileInfo(_ context.Context, id string) (*database.FileInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetHeaderByAccessionID(_ context.Context, _ string) ([]byte, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetMappingData(_ context.Context, _ string) (*database.MappingData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetSyncData(_ context.Context, _ string) (*database.SyncData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetFileIDInInbox(_ context.Context, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) CheckIfDatasetExists(_ context.Context, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetArchivePathAndLocation(_ context.Context, _ string) (string, string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetArchiveLocation(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetSubmissionFileSize(_ context.Context, _ string, _ int64) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetUserFiles(_ context.Context, _, _ string, _ bool) ([]*database.SubmissionFileInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) ListActiveUsers(_ context.Context) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetDatasetStatus(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) AddKeyHash(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetKeyHash(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetKeyHash(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) ListKeyHashes(_ context.Context) ([]*database.C4ghKeyHash, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) DeprecateKeyHash(_ context.Context, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) ListDatasets(_ context.Context) ([]*database.DatasetInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) ListUserDatasets(_ context.Context, _ string) ([]*database.DatasetInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) UpdateUserInfo(_ context.Context, _, _, _ string, _ []string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetReVerificationData(_ context.Context, _ string) (*database.ReVerificationData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetReVerificationDataFromFileID(_ context.Context, _ string) (*database.ReVerificationData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetDecryptedChecksum(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetDatasetFiles(_ context.Context, _ string) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetDatasetFileIDs(_ context.Context, _ string) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetFileDetails(_ context.Context, fileUUID, event string) (*database.FileDetails, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) SetBackedUp(_ context.Context, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetSizeAndObjectCountOfLocation(_ context.Context, location string) (uint64, uint64, error) {
	args := m.Called(location)

	return args.Get(0).(uint64), args.Get(1).(uint64), args.Error(2)
}

func (m *mockDatabase) GetUploadedSubmissionFilePathAndLocation(_ context.Context, _, _ string) (string, string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) GetSubmissionLocation(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) IsFileInDataset(_ context.Context, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *mockDatabase) CancelFile(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func TestLocationBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(LocationBrokerTestSuite))
}

func (ts *LocationBrokerTestSuite) TestGetSize() {
	mockDb := &mockDatabase{}
	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}

	size, err := lb.GetSize(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(123), size)
}

func (ts *LocationBrokerTestSuite) TestGetObjectCount() {
	mockDb := &mockDatabase{}
	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}

	count, err := lb.GetObjectCount(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(321), count)
}

func (ts *LocationBrokerTestSuite) TestGetObjectCount_WithCache() {
	mockDb := &mockDatabase{}
	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb, CacheTTL(time.Second*60))
	if err != nil {
		ts.FailNow(err.Error())
	}

	countFromDB, err := lb.GetObjectCount(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(321), countFromDB)

	countFromCache, err := lb.GetObjectCount(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(countFromDB, countFromCache)
	mockDb.AssertNumberOfCalls(ts.T(), "GetSizeAndObjectCountOfLocation", 1)
}

func (ts *LocationBrokerTestSuite) TestGetSize_WithCache() {
	mockDb := &mockDatabase{}
	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb, CacheTTL(time.Second*60))
	if err != nil {
		ts.FailNow(err.Error())
	}

	sizeFromDB, err := lb.GetObjectCount(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(321), sizeFromDB)

	sizeFromCache, err := lb.GetObjectCount(context.TODO(), "inbox", "mock_location")
	ts.NoError(err)
	ts.Equal(sizeFromDB, sizeFromCache)
	mockDb.AssertNumberOfCalls(ts.T(), "GetSizeAndObjectCountOfLocation", 1)
}

func (ts *LocationBrokerTestSuite) TestGetSize_WithDefaultFinderFunc() {
	mockDb := &mockDatabase{}

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}
	lb.RegisterSizeAndCountFinderFunc("sync", func(_ string) bool {
		return true
	}, func(_ context.Context, _ string) (uint64, uint64, error) {
		return uint64(789), uint64(987), nil
	})

	size, err := lb.GetSize(context.TODO(), "sync", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(789), size)
}

func (ts *LocationBrokerTestSuite) TestGetObjectCount_WithDefaultFinderFunc() {
	mockDb := &mockDatabase{}

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}
	lb.RegisterSizeAndCountFinderFunc("sync", func(_ string) bool {
		return true
	}, func(_ context.Context, _ string) (uint64, uint64, error) {
		return uint64(789), uint64(987), nil
	})

	size, err := lb.GetObjectCount(context.TODO(), "sync", "mock_location")
	ts.NoError(err)
	ts.Equal(uint64(987), size)
}
