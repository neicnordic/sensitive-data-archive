package file

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHelpers is a mock implementation of the helpers package functions
type MockHelpers struct {
	mock.Mock
}

// Mock the GetPagedResponseBody function
func (m *MockHelpers) GetPagedResponseBody(apiURL, token string) ([]byte, http.Header, error) {
	args := m.Called(apiURL, token)

	return args.Get(0).([]byte), args.Get(1).(http.Header), args.Error(2)
}

// Mock the PostRequest function
func (m *MockHelpers) PostRequest(apiURL, token string, jsonBody []byte) ([]byte, error) {
	args := m.Called(apiURL, token, jsonBody)

	return args.Get(0).([]byte), args.Error(1)
}

func TestList(t *testing.T) {
	mockHelpers := new(MockHelpers)
	mockHelpers.On("GetPagedResponseBody", "http://example.com/users/testuser/files", "test-token").
		Return([]byte(`["file1", "file2"]`), http.Header{}, nil)

	// Replace the original GetPagedResponseBody with the mock
	originalFunc := helpers.GetPagedResponseBody
	defer func() { helpers.GetPagedResponseBody = originalFunc }()
	helpers.GetPagedResponseBody = mockHelpers.GetPagedResponseBody

	err := List("http://example.com", "test-token", "testuser")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestIngestPath_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var ingestInfo helpers.FileInfo
	expectedURL := "http://example.com/file/ingest"
	ingestInfo.URL = "http://example.com"
	ingestInfo.Token = "test-token"
	ingestInfo.User = "test-user"
	ingestInfo.Path = "/path/to/file"
	jsonBody := []byte(`{"filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, ingestInfo.Token, jsonBody).Return([]byte(`{}`), nil)

	err := Ingest(ingestInfo)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestIngestPath_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var ingestInfo helpers.FileInfo
	expectedURL := "http://example.com/file/ingest"
	ingestInfo.URL = "http://example.com"
	ingestInfo.Token = "test-token"
	ingestInfo.User = "test-user"
	ingestInfo.Path = "/path/to/file"
	jsonBody := []byte(`{"filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, ingestInfo.Token, jsonBody).Return([]byte(nil), errors.New("failed to send request"))

	err := Ingest(ingestInfo)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestIngestID_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var ingestInfo helpers.FileInfo
	expectedURL := "http://example.com/file/ingest?fileid=dd813b8a-ea90-4556-b640-32039733a31f"
	ingestInfo.URL = "http://example.com"
	ingestInfo.Token = "test-token"
	ingestInfo.ID = "dd813b8a-ea90-4556-b640-32039733a31f"

	mockHelpers.On("PostRequest", expectedURL, ingestInfo.Token, []byte(nil)).Return([]byte(`{}`), nil)

	err := Ingest(ingestInfo)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestIngestID_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var ingestInfo helpers.FileInfo
	expectedURL := "http://example.com/file/ingest?fileid=dd813b8a-ea90-4556-b640-32039733a31f"
	ingestInfo.URL = "http://example.com"
	ingestInfo.Token = "test-token"
	ingestInfo.ID = "dd813b8a-ea90-4556-b640-32039733a31f"

	mockHelpers.On("PostRequest", expectedURL, ingestInfo.Token, []byte(nil)).Return([]byte(nil), errors.New("failed to send request"))

	err := Ingest(ingestInfo)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestSetAccessionPath_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var accessionInfo helpers.FileInfo
	expectedURL := "http://example.com/file/accession"
	accessionInfo.URL = "http://example.com"
	accessionInfo.Token = "test-token"
	accessionInfo.User = "test-user"
	accessionInfo.Path = "/path/to/file"
	accessionInfo.Accession = "accession-123"
	jsonBody := []byte(`{"accession_id":"accession-123","filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, accessionInfo.Token, jsonBody).Return([]byte(`{}`), nil)

	err := SetAccession(accessionInfo)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestSetAccessionPath_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var accessionInfo helpers.FileInfo
	expectedURL := "http://example.com/file/accession"
	accessionInfo.URL = "http://example.com"
	accessionInfo.Token = "test-token"
	accessionInfo.User = "test-user"
	accessionInfo.Path = "/path/to/file"
	accessionInfo.Accession = "accession-123"
	jsonBody := []byte(`{"accession_id":"accession-123","filepath":"/path/to/file","user":"test-user"}`)

	mockHelpers.On("PostRequest", expectedURL, accessionInfo.Token, jsonBody).Return([]byte(nil), errors.New("failed to send request"))

	err := SetAccession(accessionInfo)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestSetAccessionID_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var accessionInfo helpers.FileInfo
	expectedURL := "http://example.com/file/accession?accessionid=accession-123&fileid=dd813b8a-ea90-4556-b640-32039733a31f"
	accessionInfo.URL = "http://example.com"
	accessionInfo.Token = "test-token"
	accessionInfo.ID = "dd813b8a-ea90-4556-b640-32039733a31f"
	accessionInfo.Accession = "accession-123"

	mockHelpers.On("PostRequest", expectedURL, accessionInfo.Token, []byte(nil)).Return([]byte(`{}`), nil)

	err := SetAccession(accessionInfo)
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestSetAccessionID_PostRequestFailure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalFunc := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalFunc }() // Restore original after test

	var accessionInfo helpers.FileInfo
	expectedURL := "http://example.com/file/accession?accessionid=accession-123&fileid=dd813b8a-ea90-4556-b640-32039733a31f"
	accessionInfo.URL = "http://example.com"
	accessionInfo.Token = "test-token"
	accessionInfo.ID = "dd813b8a-ea90-4556-b640-32039733a31f"
	accessionInfo.Accession = "accession-123"

	mockHelpers.On("PostRequest", expectedURL, accessionInfo.Token, []byte(nil)).Return([]byte(nil), errors.New("failed to send request"))

	err := SetAccession(accessionInfo)
	assert.Error(t, err)
	assert.EqualError(t, err, "failed to send request")
	mockHelpers.AssertExpectations(t)
}

func TestRotateKey_Success(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalPost := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalPost }()

	apiURI := "http://example.com"
	token := "test-token"
	fileID := "file-uuid"

	mockHelpers.On("PostRequest", "http://example.com/file/rotatekey/file-uuid", "test-token", []byte(nil)).Return([]byte(`{}`), nil)

	err := RotateKey(apiURI, token, fileID)

	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}

func TestRotateKey_Failure(t *testing.T) {
	mockHelpers := new(MockHelpers)
	originalPost := helpers.PostRequest
	helpers.PostRequest = mockHelpers.PostRequest
	defer func() { helpers.PostRequest = originalPost }()

	apiURI := "http://example.com"
	token := "test-token"
	fileID := "file-uuid"

	mockHelpers.On("PostRequest", "http://example.com/file/rotatekey/file-uuid", "test-token", []byte(nil)).Return([]byte(nil), errors.New("post request failed"))

	err := RotateKey(apiURI, token, fileID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "post request failed")
	mockHelpers.AssertExpectations(t)
}

func TestListMultiPage(t *testing.T) {
	mockHelpers := new(MockHelpers)
	// Replace the original GetPagedResponseBody with the mock
	originalFunc := helpers.GetPagedResponseBody
	defer func() { helpers.GetPagedResponseBody = originalFunc }()
	helpers.GetPagedResponseBody = mockHelpers.GetPagedResponseBody

	// Replace waitForContinue so the test doesn't block on stdin
	originalWait := waitForContinue
	defer func() { waitForContinue = originalWait }()
	waitForContinue = func() error { return nil }

	// Build expected URLs the same way List does
	baseURL, _ := url.Parse("http://example.com")
	baseURL.Path = path.Join(baseURL.Path, "users", "testuser", "files")

	page2URL := *baseURL
	q2 := page2URL.Query()
	q2.Set("cursor", "tok1")
	page2URL.RawQuery = q2.Encode()

	// First page returns a cursor; second page returns none.
	mockHelpers.On("GetPagedResponseBody", baseURL.String(), "test-token").
		Return([]byte(`["file1"]`), http.Header{"X-Next-Cursor": []string{"tok1"}}, nil)
	mockHelpers.On("GetPagedResponseBody", page2URL.String(), "test-token").
		Return([]byte(`["file2"]`), http.Header{}, nil)

	// Feed stdin with a newline so waitForContinue fallback (non-tty) won't block.
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	_ = w.Close()

	err := List("http://example.com", "test-token", "testuser")
	assert.NoError(t, err)
	mockHelpers.AssertExpectations(t)
}
