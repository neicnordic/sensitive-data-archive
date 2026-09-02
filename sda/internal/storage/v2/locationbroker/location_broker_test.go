package locationbroker

import (
	"context"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/suite"
)

type LocationBrokerTestSuite struct {
	suite.Suite
}

func TestLocationBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(LocationBrokerTestSuite))
}

func (ts *LocationBrokerTestSuite) TestGetSize() {
	mockDb := &mocks.MockDatabase{}
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
	mockDb := &mocks.MockDatabase{}
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
	mockDb := &mocks.MockDatabase{}
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
	mockDb := &mocks.MockDatabase{}
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
	mockDb := &mocks.MockDatabase{}

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
	mockDb := &mocks.MockDatabase{}

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
