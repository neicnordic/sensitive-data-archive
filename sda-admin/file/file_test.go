package file

import (
	"errors"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHelpers is a mock implementation of the helpers package functions
type MockHelpers struct {
	mock.Mock
}

// Mock the GetResponseBody function
func (m *MockHelpers) GetResponseBody(url, token string) ([]byte, error) {
	args := m.Called(url, token)

	return args.Get(0).([]byte), args.Error(1)
}

// Mock the PostRequest function
func (m *MockHelpers) PostRequest(url, token string, jsonBody []byte) ([]byte, error) {
	args := m.Called(url, token, jsonBody)

	return args.Get(0).([]byte), args.Error(1)
}

func TestList(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetResponseBody", "http://example.com/users/testuser/files", "test-token").Return([]byte(`["file1", "file2"]`), nil)

	// Replace the original GetResponseBody with the mock
	originalFunc := helpers.GetResponseBody
	defer func() { helpers.GetResponseBody = originalFunc }()
	helpers.GetResponseBody = mockHelpers.GetResponseBody

	err := List("http://example.com", "test-token", "testuser")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestIngest_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/file/ingest"
	token := "test-token"
	username := "test-user"
	filepath := "/path/to/file"
	jsonBody := []byte(`{"filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(`{}`), nil)

	err := Ingest("http://example.com", token, username, filepath)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestIngest_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/file/ingest"
	token := "test-token"
	username := "test-user"
	filepath := "/path/to/file"
	jsonBody := []byte(`{"filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(nil), errors.New("failed to send request"))

	err := Ingest("http://example.com", token, username, filepath)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestSetAccession_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/file/accession"
	token := "test-token"
	username := "test-user"
	filepath := "/path/to/file"
	accessionID := "accession-123"
	jsonBody := []byte(`{"accession_id":"accession-123","filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(`{}`), nil)

	err := SetAccession("http://example.com", token, username, filepath, accessionID)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestSetAccession_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/file/accession"
	token := "test-token"
	username := "test-user"
	filepath := "/path/to/file"
	accessionID := "accession-123"
	jsonBody := []byte(`{"accession_id":"accession-123","filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(nil), errors.New("failed to send request"))

	err := SetAccession("http://example.com", token, username, filepath, accessionID)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}
