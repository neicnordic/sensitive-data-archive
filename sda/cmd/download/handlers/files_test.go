package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

// Success-path audit tests

// fakeReencryptServer answers every ReencryptHeader call with a fixed header
// and records the requests it receives.
type fakeReencryptServer struct {
	re.UnimplementedReencryptServer
	header []byte

	mu       sync.Mutex
	requests []*re.ReencryptRequest
}

func (s *fakeReencryptServer) ReencryptHeader(_ context.Context, req *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)

	return &re.ReencryptResponse{Header: s.header}, nil
}

func (s *fakeReencryptServer) received() []*re.ReencryptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.requests)
}

// setupSuccessRouter serves the file routes for one file whose permission
// check, storage read and header re-encryption all succeed, and returns the
// audit events the handlers log and the fake reencrypt server.
func setupSuccessRouter(t *testing.T, body, newHeader []byte) (*gin.Engine, *capturingLogger, *fakeReencryptServer) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	reencryptSrv := &fakeReencryptServer{header: newHeader}
	srv := grpc.NewServer()
	re.RegisterReencryptServer(srv, reencryptSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	reencryptClient := reencrypt.NewClient("localhost", lis.Addr().(*net.TCPAddr).Port)
	t.Cleanup(func() { _ = reencryptClient.Close() })

	auditLog := &capturingLogger{}
	h, err := New(
		WithDatabase(&mockDatabase{
			hasPermission: true,
			fileByID: &database.File{
				ID:              "EGAF00000000001",
				DatasetID:       "EGAD00000000001",
				SubmittedPath:   "dir/test.c4gh",
				ArchivePath:     "test.c4gh",
				ArchiveLocation: "http://s3:9000/archive",
				ArchiveSize:     int64(len(body)),
				Header:          []byte("stored crypt4gh header"),
			},
		}),
		WithStorageReader(&mockStorageReader{content: body}),
		WithReencryptClient(reencryptClient),
		WithAuditLogger(auditLog),
	)
	require.NoError(t, err)

	router := gin.New()
	router.Use(correlationIDMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKey, middleware.AuthContext{
			Subject: "user@example.org",
			// The file's dataset is not first, so the event must take it from the file
			Datasets: []string{"EGAD00000000009", "EGAD00000000001"},
		})
		c.Next()
	})
	router.GET("/files/:fileId", h.DownloadFile)
	router.GET("/files/:fileId/header", h.GetFileHeader)
	router.GET("/files/:fileId/content", h.GetFileContent)

	return router, auditLog, reencryptSrv
}

func TestFileEndpoints_SuccessAuditEvent(t *testing.T) {
	body := []byte("archived crypt4gh body")
	newHeader := []byte("re-encrypted header")
	fullFile := slices.Concat(newHeader, body)

	testCases := []struct {
		name       string
		path       string
		publicKey  bool
		rangeSpec  string
		wantStatus int
		wantBody   []byte
		wantEvent  audit.EventName
	}{
		{"full download", "/files/EGAF00000000001", true, "", http.StatusOK, fullFile, audit.EventCompleted},
		{"range download", "/files/EGAF00000000001", true, "bytes=15-24", http.StatusPartialContent, fullFile[15:25], audit.EventCompleted},
		{"header", "/files/EGAF00000000001/header", true, "", http.StatusOK, newHeader, audit.EventHeader},
		{"content", "/files/EGAF00000000001/content", false, "", http.StatusOK, body, audit.EventContent},
		{"content range", "/files/EGAF00000000001/content", false, "bytes=9-", http.StatusPartialContent, body[9:], audit.EventContent},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router, auditLog, reencryptSrv := setupSuccessRouter(t, body, newHeader)

			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			if tc.publicKey {
				req.Header.Set("X-C4GH-Public-Key", "dGVzdC1wdWJsaWMta2V5")
			}
			if tc.rangeSpec != "" {
				req.Header.Set("Range", tc.rangeSpec)
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantBody, w.Body.Bytes())

			requests := reencryptSrv.received()
			if tc.publicKey {
				require.Len(t, requests, 1, "the header should be re-encrypted once")
				assert.Equal(t, []byte("stored crypt4gh header"), requests[0].GetOldheader())
				assert.Equal(t, "dGVzdC1wdWJsaWMta2V5", requests[0].GetPublickey())
			} else {
				assert.Empty(t, requests, "content requests should not re-encrypt")
			}

			require.Len(t, auditLog.events, 1, "a successful request should log exactly one audit event")
			event := auditLog.events[0]
			assert.Equal(t, tc.wantEvent, event.Event)
			assert.Equal(t, "user@example.org", event.UserID)
			assert.Equal(t, "EGAF00000000001", event.FileID)
			assert.Equal(t, "EGAD00000000001", event.DatasetID)
			assert.Equal(t, tc.path, event.Path)
			assert.Equal(t, tc.wantStatus, event.HTTPStatus)
			assert.Empty(t, event.ErrorReason)
			assert.NotEmpty(t, event.CorrelationID)
			assert.Equal(t, w.Header().Get("X-Correlation-ID"), event.CorrelationID)
		})
	}
}
