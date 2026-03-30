package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadFile_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "KEY_MISSING", response.ErrorCode)
}

func TestDownloadFile_Unauthenticated(t *testing.T) {
	// Router without auth middleware => no AuthContext in context
	router := setupTestRouter()
	h := newTestHandlers(t)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Unauthorized", response.Title)
}

func TestDownloadFile_NonExistentFile_Returns403(t *testing.T) {
	// Verify no 404 leakage: permission check fails for non-existent file
	router := setupTestRouterWithAuth([]string{"some-dataset"})
	mockDB := &mockDatabase{
		hasPermission: false, // Permission check returns false for non-existent file
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/nonexistent-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Must be 403, not 404
	assert.Equal(t, http.StatusForbidden, w.Code)

	var response ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "access denied", response.Detail)
}

func TestDownloadFile_FileNilInDB_Returns403(t *testing.T) {
	// Permission check passes (allow-all-data or user has access) but GetFileByID returns nil
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID:      nil, // File not found in DB
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/nonexistent-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Must be 403, not 404
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDownloadFile_ReencryptNotConfigured(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:              "test-file",
			Header:          make([]byte, 20),
			ArchivePath:     "/archive/test.c4gh",
			ArchiveLocation: "s3:9000/archive",
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)
	// reencryptClient is nil

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response.Detail, "reencrypt service not configured")
}

func TestDownloadFile_FileNoHeader_Returns500(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:              "test-file",
			Header:          nil, // No header
			ArchivePath:     "/archive/test.c4gh",
			ArchiveLocation: "s3:9000/archive",
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response.Detail, "file header not available")
}

func TestDownloadFile_FileNoArchivePath_Returns500(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:          "test-file",
			Header:      make([]byte, 20),
			ArchivePath: "", // No archive path
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response.Detail, "file not in archive")
}

func TestDownloadFile_PublicKeyConflict(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/files/:fileId", h.DownloadFile)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id", nil)
	req.Header.Set("X-C4GH-Public-Key", "key1")
	req.Header.Set("Htsget-Context-Public-Key", "key2")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "KEY_CONFLICT", response.ErrorCode)
}

// HeadFile tests

func TestHeadFile_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.HEAD("/files/:fileId", h.HeadFile)

	req, _ := http.NewRequest(http.MethodHead, "/files/test-file-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHeadFile_NonExistentFile_Returns403(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"some-dataset"})
	mockDB := &mockDatabase{
		hasPermission: false,
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.HEAD("/files/:fileId", h.HeadFile)

	req, _ := http.NewRequest(http.MethodHead, "/files/nonexistent-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHeadFile_Unauthenticated(t *testing.T) {
	router := setupTestRouter()
	h := newTestHandlers(t)

	router.HEAD("/files/:fileId", h.HeadFile)

	req, _ := http.NewRequest(http.MethodHead, "/files/test-file-id", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHeadFile_ReencryptNotConfigured(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:              "test-file",
			Header:          make([]byte, 20),
			ArchivePath:     "/archive/test.c4gh",
			ArchiveLocation: "s3:9000/archive",
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.HEAD("/files/:fileId", h.HeadFile)

	req, _ := http.NewRequest(http.MethodHead, "/files/test-file", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Content-Disposition tests

// GetFileHeader tests

func TestGetFileHeader_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/files/:fileId/header", h.GetFileHeader)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id/header", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "KEY_MISSING", response.ErrorCode)
}

func TestGetFileHeader_Unauthenticated(t *testing.T) {
	router := setupTestRouter()
	h := newTestHandlers(t)

	router.GET("/files/:fileId/header", h.GetFileHeader)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id/header", nil)
	req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Unauthorized", response.Title)
}

// HeadFileHeader tests

func TestHeadFileHeader_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.HEAD("/files/:fileId/header", h.HeadFileHeader)

	req, _ := http.NewRequest(http.MethodHead, "/files/test-file-id/header", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// GetFileContent tests

func TestGetFileContent_Unauthenticated(t *testing.T) {
	router := setupTestRouter()
	h := newTestHandlers(t)

	router.GET("/files/:fileId/content", h.GetFileContent)

	req, _ := http.NewRequest(http.MethodGet, "/files/test-file-id/content", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Unauthorized", response.Title)
}

func TestGetFileContent_NonExistentFile_Returns403(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"some-dataset"})
	mockDB := &mockDatabase{
		hasPermission: false,
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/files/:fileId/content", h.GetFileContent)

	req, _ := http.NewRequest(http.MethodGet, "/files/nonexistent-file/content", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "access denied", response.Detail)
}

// HeadFileContent tests

func TestHeadFileContent_Unauthenticated(t *testing.T) {
	router := setupTestRouter()
	h := newTestHandlers(t)

	router.HEAD("/files/:fileId/content", h.HeadFileContent)

	req, _ := http.NewRequest(http.MethodHead, "/files/test-file-id/content", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHeadFileContent_NoPublicKeyRequired(t *testing.T) {
	// Content endpoints do not require a public key — verify HEAD /content works
	// with a mock that has permission and file data, but no reencrypt client.
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:              "test-file",
			ArchivePath:     "/archive/test.c4gh",
			ArchiveLocation: "s3:9000/archive",
			ArchiveSize:     1024,
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)
	// reencryptClient is nil — content endpoints don't need it

	router.HEAD("/files/:fileId/content", h.HeadFileContent)

	// No public key header set
	req, _ := http.NewRequest(http.MethodHead, "/files/test-file/content", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1024", w.Header().Get("Content-Length"))
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "bytes", w.Header().Get("Accept-Ranges"))
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=60, must-revalidate", w.Header().Get("Cache-Control"))
}

// Content-Disposition tests

func TestContentDisposition_AddsC4ghExtension(t *testing.T) {
	result := contentDisposition("/path/to/myfile.txt")
	assert.Contains(t, result, "myfile.txt.c4gh")
}

func TestContentDisposition_KeepsExistingC4ghExtension(t *testing.T) {
	result := contentDisposition("/path/to/myfile.c4gh")
	assert.Contains(t, result, "myfile.c4gh")
	// Ensure it's not doubled
	assert.NotContains(t, result, "myfile.c4gh.c4gh")
}
