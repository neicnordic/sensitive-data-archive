package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type drsObjectResponse struct {
	ID            string                `json:"id"`
	SelfURI       string                `json:"self_uri"`
	Size          int64                 `json:"size"`
	CreatedTime   string                `json:"created_time"`
	Checksums     []drsChecksumJSON     `json:"checksums"`
	AccessMethods []drsAccessMethodJSON `json:"access_methods"`
}

type drsChecksumJSON struct {
	Checksum string `json:"checksum"`
	Type     string `json:"type"`
}

type drsAccessMethodJSON struct {
	Type      string           `json:"type"`
	AccessURL drsAccessURLJSON `json:"access_url"`
}

type drsAccessURLJSON struct {
	URL string `json:"url"`
}

func TestGetDrsObject_Success(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"EGAD00001000001"})
	mockDB := &mockDatabase{
		fileByPath: &database.File{
			ID:            "urn:neic:001-002-003",
			DatasetID:     "EGAD00001000001",
			SubmittedPath: "samples/controls/sample1.bam.c4gh",
			ArchiveSize:   2097152,
			DecryptedSize: 1048576,
			CreatedAt:     time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		fileChecksums: []database.Checksum{{Type: "SHA256", Checksum: "a1b2c3d4"}},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/samples/controls/sample1.bam.c4gh", nil)
	req.Host = "download.example.org"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "private, max-age=60, must-revalidate", w.Header().Get("Cache-Control"))

	var resp drsObjectResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "urn:neic:001-002-003", resp.ID)
	assert.Equal(t, "drs://download.example.org/urn:neic:001-002-003", resp.SelfURI)
	assert.Equal(t, int64(2097152), resp.Size)
	assert.Equal(t, "2026-01-15T10:30:00Z", resp.CreatedTime)

	require.Len(t, resp.Checksums, 1)
	assert.Equal(t, "sha-256", resp.Checksums[0].Type)
	assert.Equal(t, "a1b2c3d4", resp.Checksums[0].Checksum)

	require.Len(t, resp.AccessMethods, 1)
	assert.Equal(t, "https", resp.AccessMethods[0].Type)
	assert.Equal(t, "http://download.example.org/files/urn:neic:001-002-003/content", resp.AccessMethods[0].AccessURL.URL)
}

func TestGetDrsObject_FileNotFound_Returns403(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"EGAD00001000001"})
	mockDB := &mockDatabase{
		fileByPath: nil,
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/no-such-file.bam", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "access denied", resp.Detail)
}

func TestGetDrsObject_NoDatasetAccess_Returns403(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"OTHER-DATASET"})
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/file.bam", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetDrsObject_MalformedPath_Returns400(t *testing.T) {
	testCases := []struct {
		name string
		path string
	}{
		{"no slash", "/objects/just-dataset-id"},
		{"trailing slash", "/objects/dataset/"},
		{"empty dataset", "/objects//file.bam"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := setupTestRouterWithAuth([]string{"dataset"})
			mockDB := &mockDatabase{}
			h, err := New(WithDatabase(mockDB))
			require.NoError(t, err)

			router.GET("/objects/*path", h.GetDrsObject)

			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp ProblemDetails
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Contains(t, resp.Detail, "path must contain")
		})
	}
}

func TestGetDrsObject_Unauthenticated_Returns401(t *testing.T) {
	router := setupTestRouter()
	mockDB := &mockDatabase{}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/file.bam", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetDrsObject_DBError_Returns500(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"EGAD00001000001"})
	mockDB := &mockDatabase{
		err: errors.New("db error"),
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/file.bam", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetDrsObject_NoChecksum_Returns500(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"EGAD00001000001"})
	mockDB := &mockDatabase{
		fileByPath: &database.File{
			ID:            "urn:neic:001-002-003",
			DatasetID:     "EGAD00001000001",
			SubmittedPath: "file.bam",
			DecryptedSize: 512,
			CreatedAt:     time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		fileChecksums: []database.Checksum{},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/file.bam", nil)
	req.Host = "download.example.org"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp ProblemDetails
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Detail, "no checksums")
}

func TestGetDrsObject_MultipleChecksums(t *testing.T) {
	router := setupTestRouterWithAuth([]string{"EGAD00001000001"})
	mockDB := &mockDatabase{
		fileByPath: &database.File{
			ID:            "urn:neic:001-002-003",
			DatasetID:     "EGAD00001000001",
			SubmittedPath: "file.bam",
			ArchiveSize:   4096,
			DecryptedSize: 2048,
			CreatedAt:     time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		fileChecksums: []database.Checksum{
			{Type: "SHA256", Checksum: "abc"},
			{Type: "MD5", Checksum: "def"},
		},
	}
	h, err := New(WithDatabase(mockDB))
	require.NoError(t, err)

	router.GET("/objects/*path", h.GetDrsObject)

	req, _ := http.NewRequest(http.MethodGet, "/objects/EGAD00001000001/file.bam", nil)
	req.Host = "download.example.org"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp drsObjectResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.Len(t, resp.Checksums, 2)
	assert.Equal(t, "sha-256", resp.Checksums[0].Type)
	assert.Equal(t, "abc", resp.Checksums[0].Checksum)
	assert.Equal(t, "md5", resp.Checksums[1].Type)
	assert.Equal(t, "def", resp.Checksums[1].Checksum)
}

func TestDrsChecksumType(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"SHA256", "sha-256"},
		{"sha256", "sha-256"},
		{"SHA-256", "sha-256"},
		{"SHA384", "sha-384"},
		{"SHA512", "sha-512"},
		{"MD5", "md5"},
		{"crc32c", "crc32c"},
		{"UNKNOWN", "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := drsChecksumType(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
