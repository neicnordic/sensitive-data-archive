package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthLive(t *testing.T) {
	router := gin.New()
	h := New()
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
	h := New()
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

func TestInfoDataset_MissingParameter(t *testing.T) {
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

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
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/info/dataset/files", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "dataset parameter is required")
}

func TestDownloadByFileID_MissingPublicKey(t *testing.T) {
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

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
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

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
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

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
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

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
	router := gin.New()
	h := New()
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/file?dataset=test-dataset&fileId=test-id", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "public_key header is required")
}
