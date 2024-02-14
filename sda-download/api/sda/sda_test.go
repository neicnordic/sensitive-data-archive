package sda

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/database"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/internal/storage"
)

func TestDatasets(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("datasets", session.Cache{Datasets: []string{"dataset1", "dataset2"}})

	// Test the outcomes of the handler
	Datasets(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(`["dataset1","dataset2"]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext

}

func TestFind_Found(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset2", datasets)

	// Expected results
	expectedFound := true

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestFind_NotFound(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset4", datasets)

	// Expected results
	expectedFound := false

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestGetFiles_Fail_Database(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		return nil, errors.New("something went wrong")
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 500
	expectedError := "database error"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_Database failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestGetFiles_Fail_NotFound(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset3", c)

	// Expected results
	expectedStatusCode := 404
	expectedError := "dataset not found"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
}

func TestGetFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		fileInfo := database.FileInfo{
			FileID: "file1",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, nil
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 200
	expectedFileID := "file1"

	if fileInfo[0].FileID != expectedFileID {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Success failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err != nil {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", err)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestFiles_Fail(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		return nil, 404, errors.New("dataset not found")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("dataset not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		fileInfo := database.FileInfo{
			FileID:                    "file1",
			DatasetID:                 "dataset1",
			DisplayFileName:           "file1.txt",
			FilePath:                  "dir/file1.txt",
			FileName:                  "file1.txt",
			FileSize:                  200,
			DecryptedFileSize:         100,
			DecryptedFileChecksum:     "hash",
			DecryptedFileChecksumType: "sha256",
			Status:                    "READY",
			CreatedAt:                 "a while ago",
			LastModified:              "now",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, 200, nil
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(
		`[{"fileId":"file1","datasetId":"dataset1","displayFileName":"file1.txt","filePath":` +
			`"dir/file1.txt","fileName":"file1.txt","fileSize":200,"decryptedFileSize":100,` +
			`"decryptedFileChecksum":"hash","decryptedFileChecksumType":"sha256",` +
			`"fileStatus":"READY","createdAt":"a while ago","lastModified":"now"}]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestDownload_Fail_FileNotFound(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "", errors.New("file not found")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("file not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission

}

func TestDownload_Fail_NoPermissions(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		// nolint:goconst
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 401
	expectedBody := []byte("unauthorised")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext

}

func TestDownload_Fail_GetFile(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		return nil, errors.New("database error")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("database error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_GetFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_GetFile failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile

}

func TestDownload_Fail_OpenFile(t *testing.T) {

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	Backend, _ = storage.NewBackend(config.Config.Archive)

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		fileDetails := &database.FileDownload{
			ArchivePath: "non-existant-file.txt",
			ArchiveSize: 0,
			Header:      []byte{},
		}

		return fileDetails, nil
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("archive error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_OpenFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_OpenFile failed, got '%s' expected '%s'", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile

}
