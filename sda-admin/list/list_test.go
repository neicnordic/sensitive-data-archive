package list

import (
	"errors"
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

func TestListUsers_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/users", "test-token").Return([]byte(`["user1", "user2"]`), nil)

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := ListUsers("http://example.com", "test-token")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestListUsers_Failure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/users", "test-token").Return([]byte(nil), errors.New("failed to get users"))

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := ListUsers("http://example.com", "test-token")
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to get users")
	mockHelpers.AssertExpectations(t)
}

func TestListFiles_NoUsername_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/files", "test-token").Return([]byte(`["file1", "file2"]`), nil)

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := ListFiles("http://example.com", "test-token", "")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestListFiles_WithUsername_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/users/testuser/files", "test-token").Return([]byte(`["file1", "file2"]`), nil)

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := ListFiles("http://example.com", "test-token", "testuser")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestListFiles_Failure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/files", "test-token").Return([]byte(nil), errors.New("failed to get files"))

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := ListFiles("http://example.com", "test-token", "")
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to get files")
	mockHelpers.AssertExpectations(t)
}
