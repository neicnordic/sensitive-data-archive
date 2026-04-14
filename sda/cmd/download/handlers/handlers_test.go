package handlers

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

var loadConfigOnce sync.Once

func ensureTestConfig(t *testing.T) {
	t.Helper()

	loadConfigOnce.Do(func() {
		t.Setenv("DB_HOST", "localhost")
		t.Setenv("DB_USER", "test")
		t.Setenv("DB_PASSWORD", "test")
		t.Setenv("GRPC_HOST", "localhost")
		t.Setenv("AUTH_ALLOW_OPAQUE", "true")
		t.Setenv("SERVICE_ORG_NAME", "Test")
		t.Setenv("SERVICE_ORG_URL", "https://test.example.com")

		oldArgs := os.Args
		os.Args = []string{oldArgs[0]}
		defer func() { os.Args = oldArgs }()

		require.NoError(t, configv2.Load())
	})
}

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

	// Mock middleware that injects auth context
	router.Use(func(c *gin.Context) {
		authCtx := middleware.AuthContext{
			Datasets: datasets,
		}
		c.Set(middleware.ContextKey, authCtx)
		c.Next()
	})

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

func TestHealthReady_Degraded(t *testing.T) {
	// Handler with only database (no storage or grpc) should be degraded
	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthStatus
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "degraded", response.Status)
	assert.NotEmpty(t, response.Services)
	assert.Equal(t, "ok", response.Services["database"])
	assert.Contains(t, response.Services["storage"], "not configured")
	assert.Contains(t, response.Services["grpc"], "not configured")
}

func TestHealthReady_StorageAndDBHealthy(t *testing.T) {
	router := gin.New()
	mockDB := &mockDatabase{}
	mockStorage := &mockStorageReader{}
	h, err := New(WithDatabase(mockDB), WithStorageReader(mockStorage))
	require.NoError(t, err)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthStatus
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	// Still degraded because grpc is not configured
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "ok", response.Services["database"])
	assert.Equal(t, "ok", response.Services["storage"])
	assert.Contains(t, response.Services["grpc"], "not configured")
}

func TestHealthReady_StorageUnhealthy(t *testing.T) {
	router := gin.New()
	mockDB := &mockDatabase{}
	mockStorage := &mockStorageReader{pingErr: errors.New("connection refused")}
	h, err := New(WithDatabase(mockDB), WithStorageReader(mockStorage))
	require.NoError(t, err)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthStatus
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "error: backend unresponsive", response.Services["storage"])
}

// startHealthGRPCServer starts a gRPC server with a health service reporting
// the given status. Returns the listening port and a stop function.
func startHealthGRPCServer(t *testing.T, status healthgrpc.HealthCheckResponse_ServingStatus) (int, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", status)
	healthgrpc.RegisterHealthServer(srv, healthSrv)

	go func() { _ = srv.Serve(lis) }()

	return lis.Addr().(*net.TCPAddr).Port, srv.Stop
}

func TestHealthReady_AllHealthy(t *testing.T) {
	port, stopServer := startHealthGRPCServer(t, healthgrpc.HealthCheckResponse_SERVING)
	defer stopServer()

	reencryptClient := reencrypt.NewClient("localhost", port)
	defer reencryptClient.Close()

	router := gin.New()
	h, err := New(
		WithDatabase(&mockDatabase{}),
		WithStorageReader(&mockStorageReader{}),
		WithReencryptClient(reencryptClient),
	)
	require.NoError(t, err)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthStatus
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response.Status)
	assert.Equal(t, "ok", response.Services["database"])
	assert.Equal(t, "ok", response.Services["storage"])
	assert.Equal(t, "ok", response.Services["grpc"])
}

func TestHealthReady_GrpcUnhealthy(t *testing.T) {
	port, stopServer := startHealthGRPCServer(t, healthgrpc.HealthCheckResponse_NOT_SERVING)
	defer stopServer()

	reencryptClient := reencrypt.NewClient("localhost", port)
	defer reencryptClient.Close()

	router := gin.New()
	h, err := New(
		WithDatabase(&mockDatabase{}),
		WithStorageReader(&mockStorageReader{}),
		WithReencryptClient(reencryptClient),
	)
	require.NoError(t, err)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthStatus
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "degraded", response.Status)
	assert.Equal(t, "ok", response.Services["database"])
	assert.Equal(t, "ok", response.Services["storage"])
	assert.Equal(t, "error: backend unresponsive", response.Services["grpc"])
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

// v2 route wiring tests — verify RegisterRoutes applies auth middleware correctly

func TestRoutes_AuthRequired(t *testing.T) {
	ensureTestConfig(t)
	err := middleware.InitAuth()
	require.NoError(t, err)

	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	// All dataset and file routes should return 401 without a token
	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/datasets"},
		{http.MethodGet, "/datasets/test-dataset"},
		{http.MethodGet, "/datasets/test-dataset/files"},
		{http.MethodGet, "/files/test-file-id"},
		{http.MethodHead, "/files/test-file-id"},
		{http.MethodGet, "/files/test-file-id/header"},
		{http.MethodHead, "/files/test-file-id/header"},
		{http.MethodGet, "/files/test-file-id/content"},
		{http.MethodHead, "/files/test-file-id/content"},
		{http.MethodGet, "/objects/test-dataset/test-file.c4gh"},
	}

	for _, p := range paths {
		t.Run(p.method+" "+p.path, func(t *testing.T) {
			req, _ := http.NewRequest(p.method, p.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

func TestRoutes_ServiceInfoNoAuth(t *testing.T) {
	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/service-info", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// service-info does NOT require auth
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRoutes_OldV1PathsReturn404(t *testing.T) {
	ensureTestConfig(t)
	err := middleware.InitAuth()
	require.NoError(t, err)

	router := gin.New()
	h := newTestHandlers(t)
	h.RegisterRoutes(router)

	// Old v1 routes should no longer exist
	paths := []string{
		"/info/datasets",
		"/info/dataset",
		"/info/dataset/files",
		"/file/test-file",
		"/file",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}
