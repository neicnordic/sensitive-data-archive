package user

import (
	"testing"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHelpers is a mock of the helpers package
type MockHelpers struct {
	mock.Mock
}

// Mock the GetResponseBody function
func (m *MockHelpers) GetResponseBody(url, token string) ([]byte, error) {
	args := m.Called(url, token)

	return args.Get(0).([]byte), args.Error(1)
}

func TestList(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/users", "test-token").Return([]byte(`["user1", "user2"]`), nil)

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := List("http://example.com", "test-token")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}
