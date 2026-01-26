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

func (m *mockLocationBroker) GetObjectCount(_ context.Context, location string) (uint64, error) {
	args := m.Called(location)
	count := args.Int(0)
	if count < 0 {
		count = 0
	}
	//nolint:gosec // disable G115
	return uint64(count), args.Error(1)
}

func (m *mockLocationBroker) GetSize(_ context.Context, location string) (uint64, error) {
	args := m.Called(location)
	size := args.Int(0)
	if size < 0 {
		size = 0
	}
	//nolint:gosec // disable G115
	return uint64(size), args.Error(1)
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
