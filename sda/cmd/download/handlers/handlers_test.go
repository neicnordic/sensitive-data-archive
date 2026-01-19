package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestHandlers creates handlers with a mock database for testing.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	return h
}

// setupTestRouter creates a router without auth middleware for testing handlers directly
func setupTestRouter() *gin.Engine {
	router := gin.New()

	return router
}

// setupTestRouterWithAuth creates a router with mocked auth that injects auth context
func setupTestRouterWithAuth(datasets []string) *gin.Engine {
	router := gin.New()

	// Initialize session cache
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})

	// Mock middleware that injects auth context
	router.Use(func(c *gin.Context) {
		authCtx := middleware.AuthContext{
			Datasets: datasets,
		}
		c.Set(middleware.ContextKey, authCtx)
		c.Next()
	})

	// Keep cache reference for cleanup
	_ = cache

	return router
}

// setupTestRouterWithRealAuth creates a router with real auth middleware
func setupTestRouterWithRealAuth() *gin.Engine {
	router := gin.New()

	// Initialize session cache for middleware
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	_ = cache

	return router
}

func TestHealthLive(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/live", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestHealthReady(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthStatus
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response.Status)
	assert.NotEmpty(t, response.Services)
}

// Test auth middleware blocks unauthenticated requests
func TestInfoDataset_Unauthenticated(t *testing.T) {
	// Initialize session cache for middleware
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	_ = cache
	time.Sleep(10 * time.Millisecond)

	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should be unauthorized (401) because no token provided
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInfoDataset_MissingParameter(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	// Register routes without middleware (we use our mock middleware)
	router.GET("/info/dataset", h.InfoDataset)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "dataset parameter is required")
}

func TestInfoDatasetFiles_MissingParameter(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/info/dataset/files", h.InfoDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset/files", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "dataset parameter is required")
}

func TestInfoDataset_AccessDenied(t *testing.T) {
	// User has access to "other-dataset" but not "test-dataset"
	router := setupTestRouterWithAuth([]string{"other-dataset"})
	h := newTestHandlers(t)

	router.GET("/info/dataset", h.InfoDataset)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "access denied")
}

func TestDownloadByFileID_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/file/:fileId", h.DownloadByFileID)

	req, _ := http.NewRequest(http.MethodGet, "/file/test-file-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "public_key header is required")
}

func TestDownloadByQuery_MissingDataset(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?fileId=test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "dataset parameter is required")
}

func TestDownloadByQuery_MissingFileIdentifier(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "either fileId or filePath parameter is required")
}

func TestDownloadByQuery_BothFileIdentifiers(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset&fileId=id&filePath=path", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "only one of fileId or filePath can be specified")
}

func TestDownloadByQuery_MissingPublicKey(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	h := newTestHandlers(t)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset&fileId=test-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "public_key header is required")
}

// Test that hasDatasetAccess helper works correctly
func TestHasDatasetAccess(t *testing.T) {
	testCases := []struct {
		name      string
		datasets  []string
		datasetID string
		hasAccess bool
	}{
		{
			name:      "has access",
			datasets:  []string{"dataset1", "dataset2", "dataset3"},
			datasetID: "dataset2",
			hasAccess: true,
		},
		{
			name:      "no access",
			datasets:  []string{"dataset1", "dataset2"},
			datasetID: "dataset3",
			hasAccess: false,
		},
		{
			name:      "empty datasets",
			datasets:  []string{},
			datasetID: "dataset1",
			hasAccess: false,
		},
		{
			name:      "single dataset match",
			datasets:  []string{"dataset1"},
			datasetID: "dataset1",
			hasAccess: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := hasDatasetAccess(tc.datasets, tc.datasetID)
			assert.Equal(t, tc.hasAccess, result)
		})
	}
}

// InfoDatasets tests

func TestInfoDatasets_Success(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"dataset1", "dataset2"})
	mockDB := &mockDatabase{
		datasets: []database.Dataset{
			{ID: "dataset1", Title: "Dataset 1"},
			{ID: "dataset2", Title: "Dataset 2"},
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/datasets", h.InfoDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/info/datasets", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []database.Dataset
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 2)
}

func TestInfoDatasets_Empty(t *testing.T) {
	router := setupTestRouterWithAuth([]string{})
	mockDB := &mockDatabase{
		datasets: []database.Dataset{},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/datasets", h.InfoDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/info/datasets", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []database.Dataset
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 0)
}

// InfoDataset tests

func TestInfoDataset_Success(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		datasetInfo: &database.DatasetInfo{
			ID:        "test-dataset",
			Title:     "Test Dataset",
			FileCount: 10,
			TotalSize: 1024000,
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/dataset", h.InfoDataset)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response database.DatasetInfo
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-dataset", response.ID)
	assert.Equal(t, 10, response.FileCount)
}

func TestInfoDataset_NotFound(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		datasetInfo: nil, // Not found
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/dataset", h.InfoDataset)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// InfoDatasetFiles tests

func TestInfoDatasetFiles_Success(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		datasetFiles: []database.File{
			{ID: "file1", SubmittedPath: "path/to/file1.c4gh"},
			{ID: "file2", SubmittedPath: "path/to/file2.c4gh"},
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/dataset/files", h.InfoDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset/files?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []database.File
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 2)
}

func TestInfoDatasetFiles_AccessDenied(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"other-dataset"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/info/dataset/files", h.InfoDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset/files?dataset=test-dataset", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// DownloadByFileID tests

func TestDownloadByFileID_FileNotFound(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID:      nil, // Not found
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/file/:fileId", h.DownloadByFileID)

	req, _ := http.NewRequest(http.MethodGet, "/file/nonexistent-id", nil)
	req.Header.Set("public_key", "dGVzdC1wdWJsaWMta2V5") // base64 encoded
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDownloadByFileID_AccessDenied(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"other-dataset"})
	mockDB := &mockDatabase{
		hasPermission: false,
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/file/:fileId", h.DownloadByFileID)

	req, _ := http.NewRequest(http.MethodGet, "/file/test-file-id", nil)
	req.Header.Set("public_key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDownloadByFileID_StorageNotConfigured(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		hasPermission: true,
		fileByID: &database.File{
			ID:          "test-file",
			Header:      []byte("header"),
			ArchivePath: "/archive/test.c4gh",
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)
	// Note: storageReader is nil

	router.GET("/file/:fileId", h.DownloadByFileID)

	req, _ := http.NewRequest(http.MethodGet, "/file/test-file", nil)
	req.Header.Set("public_key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "storage not configured")
}

// DownloadByQuery tests

func TestDownloadByQuery_ByFileID_FileNotFound(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		fileByID: nil, // Not found
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset&fileId=nonexistent", nil)
	req.Header.Set("public_key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDownloadByQuery_ByFilePath_FileNotFound(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"test-dataset"})
	mockDB := &mockDatabase{
		fileByPath: nil, // Not found
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/file", h.DownloadByQuery)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset&filePath=nonexistent/path.c4gh", nil)
	req.Header.Set("public_key", "dGVzdC1wdWJsaWMta2V5")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
