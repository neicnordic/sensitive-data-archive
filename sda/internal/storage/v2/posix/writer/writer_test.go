package writer

import (
	"bytes"
	"context"
	"fmt"
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

func TestReaderTestSuite(t *testing.T) {
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

func (ts *WriterTestSuite) TestWriteFile_AllEmpty() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", ts.dir1)
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", ts.dir2)
	readContent, err := os.ReadFile(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.NoError(err)
	ts.Equal(content, string(readContent))
	ts.Equal(ts.dir1, location)
}
func (ts *WriterTestSuite) TestWriteFile_FirstDirFull() {
	content := "test file 2"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(11, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", ts.dir2).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir2).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_2.txt", contentReader)
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
}

func (ts *WriterTestSuite) TestRemoveFile() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", ts.dir1).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", ts.dir1).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	_, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	err = ts.writer.RemoveFile(context.TODO(), ts.dir1, "test_file_1.txt")
	ts.NoError(err)
	_, err = os.ReadFile(filepath.Join(ts.dir1, "test_file_1.txt"))
	ts.ErrorContains(err, "no such file or directory")
}
func (ts *WriterTestSuite) TestRemoveFile_LocationNotConfigured() {
	err := ts.writer.RemoveFile(context.TODO(), "/tmp/no_access_here", "test_file_1.txt")
	ts.EqualError(err, storageerrors.ErrorNoEndpointConfiguredForLocation.Error())
}
