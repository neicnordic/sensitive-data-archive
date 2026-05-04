package writer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type WriterTestSuite struct {
	suite.Suite
	writer *Writer

	configDir string

	dir1, dir2         string
	locationBrokerMock *mockLocationBroker
}

type mockLocationBroker struct {
	mock.Mock
}

func (m *mockLocationBroker) GetObjectCount(_ context.Context, _, location string) (uint64, error) {
	args := m.Called(location)
	count := args.Int(0)
	if count < 0 {
		count = 0
	}
	//nolint:gosec // disable G115
	return uint64(count), args.Error(1)
}

func (m *mockLocationBroker) GetSize(_ context.Context, _, location string) (uint64, error) {
	args := m.Called(location)
	size := args.Int(0)
	if size < 0 {
		size = 0
	}
	//nolint:gosec // disable G115
	return uint64(size), args.Error(1)
}
func (m *mockLocationBroker) RegisterSizeAndCountFinderFunc(_ string, _ func(string) bool, _ func(context.Context, string) (uint64, uint64, error)) {
	_ = m.Called()
}

func TestWriterTestSuite(t *testing.T) {
	suite.Run(t, new(WriterTestSuite))
}

func (ts *WriterTestSuite) SetupTest() {
	ts.configDir = ts.T().TempDir()
	ts.dir1 = ts.T().TempDir()
	ts.dir2 = ts.T().TempDir()
	ts.locationBrokerMock = &mockLocationBroker{}

	if err := os.WriteFile(filepath.Join(ts.configDir, "config.yaml"), []byte(fmt.Sprintf(`
storage:
  test:
    posix:
    - path: %s
      max_objects: 10
      max_size: 10kb
    - path: %s
      max_objects: 5
      max_size: 5kb
`, ts.dir1, ts.dir2)), 0600); err != nil {
		ts.FailNow(err.Error())
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetConfigFile(filepath.Join(ts.configDir, "config.yaml"))

	if err := viper.ReadInConfig(); err != nil {
		ts.FailNow(err.Error())
	}
	var err error
	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", ts.dir2).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir2).Return(0, nil).Once()
	ts.locationBrokerMock.On("RegisterSizeAndCountFinderFunc").Return().Once()
	ts.writer, err = NewWriter(context.TODO(), "test", ts.locationBrokerMock)
	if err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *WriterTestSuite) TestWriteFile() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", bytes.NewReader([]byte(content)))
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir1)
	readContent, err := os.ReadFile(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	ts.Equal(ts.dir1, location)
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir1, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
}
func (ts *WriterTestSuite) TestWriteFile_FaultyContentReader() {
	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte("partial file content"))
		_ = writer.CloseWithError(errors.New("mock error"))
	}()

	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", reader)
	ts.ErrorContains(err, "mock error")
	ts.ErrorContains(err, "failed to write to file")
	ts.Equal("", location)

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir1)
	_, err = os.Stat(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.ErrorIs(err, fs.ErrNotExist)
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir1, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)

	_ = reader.Close()
}
func (ts *WriterTestSuite) TestWriteFile_FileExist_FaultyContentReader() {
	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Twice()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Twice()

	content := "test file 1 not be overwritten by writeFile with err"
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", bytes.NewReader([]byte(content)))
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Equal(ts.dir1, location)

	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte("partial file content"))
		_ = writer.CloseWithError(errors.New("mock error"))
	}()

	location, err = ts.writer.WriteFile(context.TODO(), "test_file_1.txt", reader)
	ts.ErrorContains(err, "mock error")
	ts.ErrorContains(err, "failed to write to file")
	ts.Equal("", location)

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir1)

	readContent, err := os.ReadFile(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir1, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
	_ = reader.Close()
}
func (ts *WriterTestSuite) TestWriteFile_OverWriteFile() {
	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Twice()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Twice()

	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", bytes.NewReader([]byte("test file 1 to be overwritten")))
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Equal(ts.dir1, location)

	content := "new file content"
	location, err = ts.writer.WriteFile(context.TODO(), "test_file_1.txt", bytes.NewReader([]byte(content)))
	ts.NoError(err)
	ts.Equal(ts.dir1, location)

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir1)

	readContent, err := os.ReadFile(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir1, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
}

func (ts *WriterTestSuite) TestWriteFile_InSubDirectory() {
	content := "test file 1 in sub directory"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	location, err := ts.writer.WriteFile(context.TODO(), "subdir1/subdi2/test_file_1.txt", bytes.NewReader([]byte(content)))
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir2)
	readContent, err := os.ReadFile(filepath.Join(ts.dir1, "subdir1/subdi2/test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	ts.Equal(ts.dir1, location)
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir1, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
}
func (ts *WriterTestSuite) TestWriteFile_FirstDirFull() {
	content := "test file 2"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(11, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", ts.dir2).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir2).Return(0, nil).Once()

	location, err := ts.writer.WriteFile(context.TODO(), "test_file_2.txt", bytes.NewReader([]byte(content)))
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir2)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir2)
	readContent, err := os.ReadFile(filepath.Join(ts.dir2, "test_file_2.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	ts.Equal(ts.dir2, location)
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir2, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
}

func (ts *WriterTestSuite) TestRemoveFile() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	if _, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", bytes.NewReader([]byte(content))); err != nil {
		ts.FailNow(err.Error())
	}

	ts.NoError(ts.writer.RemoveFile(context.TODO(), ts.dir1, "test_file_1.txt"))
	_, err := os.Stat(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.ErrorIs(err, fs.ErrNotExist)
}

func (ts *WriterTestSuite) TestRemoveFile_InSubDirectory() {
	content := "test file"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Twice()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Twice()

	// Create two test files in different directories
	rootParentDir := "dir1"
	dirInRootParent1 := "subdir2"
	dirInRootParent2 := "subdir3"
	file1 := filepath.Join(rootParentDir, dirInRootParent1, "test_file.txt")
	file2 := filepath.Join(rootParentDir, dirInRootParent2, "test_file.txt")

	if _, err := ts.writer.WriteFile(context.TODO(), file1, bytes.NewReader([]byte(content))); err != nil {
		ts.FailNow(err.Error())
	}
	if _, err := ts.writer.WriteFile(context.TODO(), file2, bytes.NewReader([]byte(content))); err != nil {
		ts.FailNow(err.Error())
	}

	// Delete one file
	ts.NoError(ts.writer.RemoveFile(context.TODO(), ts.dir1, file1))
	_, err := os.Stat(filepath.Join(ts.dir1, file1))
	ts.ErrorIs(err, fs.ErrNotExist)

	// Check that the parent directory "dubdir2" is deleted, but subdir1 remains
	_, err = os.Stat(filepath.Join(ts.dir1, filepath.Join(rootParentDir, dirInRootParent1)))
	ts.ErrorIs(err, fs.ErrNotExist)
	_, err = os.Stat(filepath.Join(ts.dir1, rootParentDir))
	ts.NoError(err)

	// Delete one other file
	ts.NoError(ts.writer.RemoveFile(context.TODO(), ts.dir1, file2))
	_, err = os.Stat(filepath.Join(ts.dir1, file2))
	ts.ErrorIs(err, fs.ErrNotExist)

	// Check that all sub directories are now deleted since no files remain within
	_, err = os.Stat(filepath.Join(ts.dir1, filepath.Join(rootParentDir, dirInRootParent2)))
	ts.ErrorIs(err, fs.ErrNotExist)
	_, err = os.Stat(filepath.Join(ts.dir1, rootParentDir))
	ts.ErrorIs(err, fs.ErrNotExist)
}
func (ts *WriterTestSuite) TestRemoveFile_LocationNotConfigured() {
	err := ts.writer.RemoveFile(context.TODO(), "/tmp/no_access_here", "test_file_1.txt")
	ts.EqualError(err, storageerrors.ErrorNoEndpointConfiguredForLocation.Error())
}

func (ts *WriterTestSuite) TestWriteFile_NoMockLocationBroker_FirstDirFull() {
	content := "test file 2"
	lb, err := locationbroker.NewLocationBroker(&notImplementedDatabase{}, locationbroker.CacheTTL(0))
	ts.NoError(err)

	writer, err := NewWriter(context.TODO(), "test", lb)
	if err != nil {
		ts.FailNow(err.Error())
	}

	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(ts.dir1, fmt.Sprintf("file-%d.txt", i)), []byte(fmt.Sprintf("file content %d", i)), 0600); err != nil {
			ts.FailNow(err.Error())
		}
	}

	contentReader := bytes.NewReader([]byte(content))
	location, err := writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	readContent, err := os.ReadFile(filepath.Join(ts.dir2, "test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	ts.Equal(ts.dir2, location)
	// Ensure location/tmp folder empty
	tmpFiles, err := os.ReadDir(filepath.Join(ts.dir2, "tmp"))
	ts.NoError(err)
	ts.Len(tmpFiles, 0)
}

type notImplementedDatabase struct {
}

func (m *notImplementedDatabase) BeginTransaction(_ context.Context) (database.Transaction, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) Close() error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SchemaVersion() (int, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) Ping(_ context.Context) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) RegisterFile(_ context.Context, _x *string, _, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetFileIDByUserPathAndStatus(_ context.Context, _, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) CheckAccessionIDOwnedByUser(_ context.Context, _, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) UpdateFileEventLog(_ context.Context, _, _, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) StoreHeader(_ context.Context, _ []byte, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) RotateHeaderKey(_ context.Context, _ []byte, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetArchived(_ context.Context, _ string, _ *database.FileInfo, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetFileStatus(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetHeader(_ context.Context, _ string) ([]byte, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) BackupHeader(_ context.Context, _ string, _ []byte, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetVerified(_ context.Context, _ *database.FileInfo, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetArchived(_ context.Context, _ string) (*database.ArchiveData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) CheckAccessionIDExists(_ context.Context, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetAccessionID(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetAccessionID(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) MapFileToDataset(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetInboxPath(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) UpdateDatasetEvent(_ context.Context, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetFileInfo(_ context.Context, id string) (*database.FileInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetHeaderByAccessionID(_ context.Context, _ string) ([]byte, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetMappingData(_ context.Context, _ string) (*database.MappingData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetSyncData(_ context.Context, _ string) (*database.SyncData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetFileIDInInbox(_ context.Context, _, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) CheckIfDatasetExists(_ context.Context, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetArchivePathAndLocation(_ context.Context, _ string) (string, string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetArchiveLocation(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetSubmissionFileSize(_ context.Context, _ string, _ int64) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetUserFiles(_ context.Context, _, _ string, _ bool) ([]*database.SubmissionFileInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) ListActiveUsers(_ context.Context) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetDatasetStatus(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) AddKeyHash(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetKeyHash(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetKeyHash(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) ListKeyHashes(_ context.Context) ([]*database.C4ghKeyHash, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) DeprecateKeyHash(_ context.Context, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) ListDatasets(_ context.Context) ([]*database.DatasetInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) ListUserDatasets(_ context.Context, _ string) ([]*database.DatasetInfo, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) UpdateUserInfo(_ context.Context, _, _, _ string, _ []string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetReVerificationData(_ context.Context, _ string) (*database.ReVerificationData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetReVerificationDataFromFileID(_ context.Context, _ string) (*database.ReVerificationData, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetDecryptedChecksum(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetDatasetFiles(_ context.Context, _ string) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetDatasetFileIDs(_ context.Context, _ string) ([]string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetFileDetails(_ context.Context, fileUUID, event string) (*database.FileDetails, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) SetBackedUp(_ context.Context, _, _, _ string) error {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetSizeAndObjectCountOfLocation(_ context.Context, location string) (uint64, uint64, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetUploadedSubmissionFilePathAndLocation(_ context.Context, _, _ string) (string, string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) GetSubmissionLocation(_ context.Context, _ string) (string, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) IsFileInDataset(_ context.Context, _ string) (bool, error) {
	panic("function not expected to be called in unit tests")
}

func (m *notImplementedDatabase) CancelFile(_ context.Context, _, _ string) error {
	panic("function not expected to be called in unit tests")
}
