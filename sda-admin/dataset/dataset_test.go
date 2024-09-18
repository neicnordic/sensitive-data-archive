package dataset

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

func TestCreate_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/dataset/create"
	token := "test-token"
	datasetID := "dataset-123"
	accessionIDs := []string{"accession-1", "accession-2"}
	jsonBody := []byte(`{"accession_ids":["accession-1","accession-2"],"dataset_id":"dataset-123"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(`{}`), nil)

	err := Create("http://example.com", token, datasetID, accessionIDs)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestCreate_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/dataset/create"
	token := "test-token"
	datasetID := "dataset-123"
	accessionIDs := []string{"accession-1", "accession-2"}
	jsonBody := []byte(`{"accession_ids":["accession-1","accession-2"],"dataset_id":"dataset-123"}`)

	mockHelpers.On("PostRequest", expectedURL, token, jsonBody).Return([]byte(nil), errors.New("failed to send request"))

	err := Create("http://example.com", token, datasetID, accessionIDs)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestRelease_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/dataset/release/dataset-123"
	token := "test-token"

	mockHelpers.On("PostRequest", expectedURL, token, []byte(nil)).Return([]byte(`{}`), nil)

	err := Release("http://example.com", token, "dataset-123")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestRelease_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	expectedURL := "http://example.com/dataset/release/dataset-123"
	token := "test-token"

	mockHelpers.On("PostRequest", expectedURL, token, []byte(nil)).Return([]byte(nil), errors.New("failed to send request"))

	err := Release("http://example.com", token, "dataset-123")
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}
