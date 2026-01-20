package locationbroker

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type LocationBrokerTestSuite struct {
	suite.Suite
	tempDir string
}

type mockDatabase struct {
	mock.Mock
}

func (m *mockDatabase) GetSizeAndObjectCountOfLocation(_ context.Context, location string) (uint64, uint64, error) {
	args := m.Called(location)

	return args.Get(0).(uint64), args.Get(1).(uint64), args.Error(2)
}

func TestLocationBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(LocationBrokerTestSuite))
}

func (ts *LocationBrokerTestSuite) SetupSuite() {
	ts.tempDir = ts.T().TempDir()

	for i := 0; i < 5; i++ {
		file, err := os.CreateTemp(ts.tempDir, "root_file")
		if err != nil {
			_ = file.Close()
			ts.FailNow(err.Error())
		}
		if _, err := file.Write([]byte(fmt.Sprintf("file content in root dir: %d", i))); err != nil {
			_ = file.Close()
			ts.FailNow(err.Error())
		}
		_ = file.Close()
	}

	for i := 0; i < 5; i++ {
		subDir, err := os.MkdirTemp(ts.tempDir, "sub_dir")
		if err != nil {
			ts.FailNow(err.Error())
		}
		if err := ts.createDummyDirectoriesAndFiles(subDir, 0, 5); err != nil {
			ts.FailNow(err.Error())
		}
	}

}

func (ts *LocationBrokerTestSuite) createDummyDirectoriesAndFiles(path string, currentDept, depth int) error {
	if currentDept >= depth {
		return nil
	}
	for i := 0; i < 5; i++ {
		subDir, err := os.MkdirTemp(path, "sub_dir")
		if err != nil {
			ts.FailNow(err.Error())
		}
		for i := 0; i < 3; i++ {
			file, err := os.CreateTemp(subDir, "sub_dir_file")
			if err != nil {
				_ = file.Close()
				ts.FailNow(err.Error())
			}
			if _, err := file.Write([]byte(fmt.Sprintf("file content in sub dir: %d", i))); err != nil {
				_ = file.Close()
				ts.FailNow(err.Error())
			}
			_ = file.Close()
		}
		return ts.createDummyDirectoriesAndFiles(subDir, currentDept+1, depth)
	}
	return nil
}

func (ts *LocationBrokerTestSuite) TestGetSizeAndCountInDir() {

	size, count, err := getSizeAndCountInDir(ts.tempDir)
	ts.NoError(err)

	ts.Equal(uint64(2085), size)
	ts.Equal(uint64(80), count)
}

func (ts *LocationBrokerTestSuite) TestGetSize_FromDir() {

	lb, err := NewLocationBroker(&mockDatabase{})
	if err != nil {
		ts.FailNow(err.Error())
	}

	size, err := lb.GetSize(context.TODO(), ts.tempDir)
	ts.NoError(err)

	ts.Equal(uint64(2085), size)
}

func (ts *LocationBrokerTestSuite) TestGetObjectCount_FromDir() {

	lb, err := NewLocationBroker(&mockDatabase{})
	if err != nil {
		ts.FailNow(err.Error())
	}

	size, err := lb.GetObjectCount(context.TODO(), ts.tempDir)
	ts.NoError(err)

	ts.Equal(uint64(80), size)
}

func (ts *LocationBrokerTestSuite) TestGetSize_FromMockDB() {

	mockDb := &mockDatabase{}

	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}

	size, err := lb.GetSize(context.TODO(), "mock_location")
	ts.NoError(err)

	ts.Equal(uint64(123), size)
}

func (ts *LocationBrokerTestSuite) TestGetObjectCount_FromMockDB() {

	mockDb := &mockDatabase{}

	mockDb.On("GetSizeAndObjectCountOfLocation", "mock_location").Return(uint64(123), uint64(321), nil).Once()

	lb, err := NewLocationBroker(mockDb)
	if err != nil {
		ts.FailNow(err.Error())
	}

	count, err := lb.GetObjectCount(context.TODO(), "mock_location")
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

	countFromDB, err := lb.GetObjectCount(context.TODO(), "mock_location")
	ts.NoError(err)

	ts.Equal(uint64(321), countFromDB)

	countFromCache, err := lb.GetObjectCount(context.TODO(), "mock_location")
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

	sizeFromDB, err := lb.GetObjectCount(context.TODO(), "mock_location")
	ts.NoError(err)

	ts.Equal(uint64(321), sizeFromDB)

	sizeFromCache, err := lb.GetObjectCount(context.TODO(), "mock_location")
	ts.NoError(err)

	ts.Equal(sizeFromDB, sizeFromCache)

	mockDb.AssertNumberOfCalls(ts.T(), "GetSizeAndObjectCountOfLocation", 1)
}
