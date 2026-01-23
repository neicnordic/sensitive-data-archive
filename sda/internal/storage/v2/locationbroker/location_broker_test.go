package locationbroker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type LocationBrokerTestSuite struct {
	suite.Suite
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

func (ts *LocationBrokerTestSuite) TestGetSize() {
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

func (ts *LocationBrokerTestSuite) TestGetObjectCount() {
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
