package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listDatasetsResponse matches the JSON shape of ListDatasets.
type listDatasetsResponse struct {
	Datasets      []string `json:"datasets"`
	NextPageToken *string  `json:"nextPageToken"`
}

// getDatasetResponse matches the JSON shape of GetDataset.
type getDatasetResponse struct {
	DatasetID string `json:"datasetId"`
	Date      string `json:"date"`
	Files     int    `json:"files"`
	Size      int64  `json:"size"`
}

// listDatasetFilesResponse matches the JSON shape of ListDatasetFiles.
type listDatasetFilesResponse struct {
	Files         []fileInfo `json:"files"`
	NextPageToken *string    `json:"nextPageToken"`
}

// --- ListDatasets tests ---

func TestListDatasets_ReturnsSortedIDs(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"c-dataset", "a-dataset", "b-dataset"})
	mockDB := &mockDatabase{
		datasets: []database.Dataset{
			{ID: "c-dataset"},
			{ID: "a-dataset"},
			{ID: "b-dataset"},
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "private, max-age=60, must-revalidate", w.Header().Get("Cache-Control"))

	var resp listDatasetsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"a-dataset", "b-dataset", "c-dataset"}, resp.Datasets)
	assert.Nil(t, resp.NextPageToken)
}

func TestListDatasets_Pagination(t *testing.T) {
	// 5 datasets, page size 2 => pages of [a,b], [c,d], [e]
	ids := []string{"e", "d", "c", "b", "a"}
	datasets := make([]database.Dataset, len(ids))
	for i, id := range ids {
		datasets[i] = database.Dataset{ID: id}
	}

	mockDB := &mockDatabase{datasets: datasets}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	// Page 1
	router := setupTestRouterWithAuth(ids)
	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets?pageSize=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp1 listDatasetsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp1))
	assert.Equal(t, []string{"a", "b"}, resp1.Datasets)
	require.NotNil(t, resp1.NextPageToken)

	// Page 2
	req, _ = http.NewRequest(http.MethodGet, "/datasets?pageSize=2&pageToken="+*resp1.NextPageToken, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp2 listDatasetsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp2))
	assert.Equal(t, []string{"c", "d"}, resp2.Datasets)
	require.NotNil(t, resp2.NextPageToken)

	// Page 3 (last)
	req, _ = http.NewRequest(http.MethodGet, "/datasets?pageSize=2&pageToken="+*resp2.NextPageToken, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp3 listDatasetsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp3))
	assert.Equal(t, []string{"e"}, resp3.Datasets)
	assert.Nil(t, resp3.NextPageToken)
}

func TestListDatasets_Unauthenticated(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListDatasets_DatabaseError(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{err: assert.AnError}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListDatasets_EmptyResult(t *testing.T) {
	router := setupTestRouterWithAuth([]string{})
	mockDB := &mockDatabase{datasets: []database.Dataset{}}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listDatasetsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Datasets)
	assert.Nil(t, resp.NextPageToken)
}

func TestListDatasets_InvalidPageSize(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{datasets: []database.Dataset{{ID: "ds1"}}}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets", h.ListDatasets)

	req, _ := http.NewRequest(http.MethodGet, "/datasets?pageSize=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- GetDataset tests ---

func TestGetDataset_Success(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	router := setupTestRouterWithAuth([]string{"EGAD00000000001"})
	mockDB := &mockDatabase{
		datasetInfo: &database.DatasetInfo{
			ID:        "EGAD00000000001",
			FileCount: 42,
			TotalSize: 999999,
			CreatedAt: ts,
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/EGAD00000000001", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp getDatasetResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "EGAD00000000001", resp.DatasetID)
	assert.Equal(t, 42, resp.Files)
	assert.Equal(t, int64(999999), resp.Size)
	assert.Equal(t, "2024-06-15T12:00:00Z", resp.Date)
}

func TestGetDataset_NoAccess(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"other-dataset"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/secret-dataset", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetDataset_NotFoundReturns403(t *testing.T) {
	// User has dataset in their access list, but DB returns nil (doesn't actually exist).
	// Should still return 403 to avoid leaking existence.
	router := setupTestRouterWithAuth([]string{"ghost-dataset"})
	mockDB := &mockDatabase{datasetInfo: nil}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ghost-dataset", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetDataset_NoAccess_EmitsAuditDenied(t *testing.T) {
	logger := &capturingLogger{}
	router := gin.New()
	// Wire correlation ID middleware + mock auth with subject
	router.Use(correlationIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKey, middleware.AuthContext{
			Subject:  "test-user",
			Datasets: []string{"other-dataset"},
		})
		c.Next()
	})

	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB), WithAuditLogger(logger))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/secret-dataset", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	require.Len(t, logger.events, 1)
	evt := logger.events[0]
	assert.Equal(t, audit.EventDenied, evt.Event)
	assert.Equal(t, http.StatusForbidden, evt.HTTPStatus)
	assert.Equal(t, "/datasets/secret-dataset", evt.Path)
	assert.Equal(t, "test-user", evt.UserID)
	assert.NotEmpty(t, evt.CorrelationID)
}

func TestGetDataset_Unauthenticated(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/any", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetDataset_DatabaseError(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{err: assert.AnError}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId", h.GetDataset)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- ListDatasetFiles tests ---

func TestListDatasetFiles_Success(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{
		datasetFilesPaged: []database.File{
			{
				ID:            "file-1",
				SubmittedPath: "data/sample.c4gh",
				ArchiveSize:   5000,
				DecryptedSize: 4096,
				Checksums: []database.Checksum{
					{Type: "sha256", Checksum: "abc123"},
				},
			},
			{
				ID:            "file-2",
				SubmittedPath: "data/other.c4gh",
				ArchiveSize:   9000,
				DecryptedSize: 8192,
			},
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listDatasetFilesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Files, 2)

	// First file has checksums
	assert.Equal(t, "file-1", resp.Files[0].FileID)
	assert.Equal(t, "data/sample.c4gh", resp.Files[0].FilePath)
	assert.Equal(t, int64(5000), resp.Files[0].Size)
	assert.Equal(t, int64(4096), resp.Files[0].DecryptedSize)
	assert.Equal(t, "/files/file-1", resp.Files[0].DownloadURL)
	require.Len(t, resp.Files[0].Checksums, 1)
	assert.Equal(t, "sha256", resp.Files[0].Checksums[0].Type)

	// Second file has empty checksums array (not null)
	assert.Equal(t, "file-2", resp.Files[1].FileID)
	assert.NotNil(t, resp.Files[1].Checksums)
	assert.Len(t, resp.Files[1].Checksums, 0)

	assert.Nil(t, resp.NextPageToken)
}

func TestListDatasetFiles_Pagination(t *testing.T) {
	// Mock returns pageSize+1 items to signal next page
	files := make([]database.File, 3)
	for i := range files {
		files[i] = database.File{
			ID:            "f" + string(rune('0'+i)),
			SubmittedPath: "path/" + string(rune('a'+i)),
		}
	}

	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{datasetFilesPaged: files}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files?pageSize=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listDatasetFilesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// Should return only pageSize items
	assert.Len(t, resp.Files, 2)
	// Should have next page token
	require.NotNil(t, resp.NextPageToken)
}

func TestListDatasetFiles_FilterConflict(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files?filePath=a&pathPrefix=b", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp ProblemDetails
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "FILTER_CONFLICT", resp.ErrorCode)
}

func TestListDatasetFiles_NoAccess(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"other"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/secret/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestListDatasetFiles_NotFoundReturns403(t *testing.T) {
	// User has dataset in their access list, but it doesn't exist in DB.
	// Should return 403 (consistent with GetDataset — no existence leakage).
	router := setupTestRouterWithAuth([]string{"ghost-dataset"})
	mockDB := &mockDatabase{datasetNotFound: true}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ghost-dataset/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestListDatasetFiles_Unauthenticated(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListDatasetFiles_DatabaseError(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{err: assert.AnError}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListDatasetFiles_InvalidPageToken(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files?pageToken=bogus", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListDatasetFiles_EmptyResult(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"ds1"})
	mockDB := &mockDatabase{datasetFilesPaged: []database.File{}}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/datasets/:datasetId/files", h.ListDatasetFiles)

	req, _ := http.NewRequest(http.MethodGet, "/datasets/ds1/files", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listDatasetFilesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Files)
	assert.Nil(t, resp.NextPageToken)
}
