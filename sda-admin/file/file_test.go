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

func (m *MockHelpers) PostRequest(url, token string, jsonBody []byte) ([]byte, error) {
	args := m.Called(url, token, jsonBody)

	return args.Get(0).([]byte), args.Error(1)
}

func TestFileIngest_Success(t *testing.T) {
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

	err := FileIngest("http://example.com", token, username, filepath)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestFileIngest_PostRequestFailure(t *testing.T) {
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

	err := FileIngest("http://example.com", token, username, filepath)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestFileAccession_Success(t *testing.T) {
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

	err := FileAccession("http://example.com", token, username, filepath, accessionID)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestFileAccession_PostRequestFailure(t *testing.T) {
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

	err := FileAccession("http://example.com", token, username, filepath, accessionID)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}
