package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/neicnordic/sensitive-data-archive/internal/audit"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureAudit points the api audit logger at a buffer for the duration of the test.
func captureAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := api.auditLogger
	api.auditLogger = audit.NewStdoutLoggerWithWriter(&buf)
	t.Cleanup(func() { api.auditLogger = prev })

	return &buf
}

// auditLines decodes every JSON line in buf as a generic map.
func auditLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		lines = append(lines, m)
	}

	return lines
}

// scopedMockDB returns the shared mock DB and drops the expectations the test adds when it ends.
func scopedMockDB(t *testing.T) *mocks.MockDatabase {
	t.Helper()
	mockDB := api.db.(*mocks.MockDatabase)
	n := len(mockDB.ExpectedCalls)
	t.Cleanup(func() { mockDB.ExpectedCalls = mockDB.ExpectedCalls[:n] })

	return mockDB
}

// allowAllRoutes lets the test user through every route, so the handlers run instead of rbac denying them.
func allowAllRoutes(t *testing.T) {
	t.Helper()
	allowAll := []byte(`{"policy":[{"role":"dummy","path":"/*","action":"(GET)|(POST)|(PUT)|(DELETE)"}],"roles":[]}`)
	m, err := model.NewModelFromString(jsonadapter.Model)
	require.NoError(t, err)
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&allowAll))
	require.NoError(t, err)
	prevEnforcer := api.enforcer
	api.enforcer = e
	t.Cleanup(func() { api.enforcer = prevEnforcer })
}

func TestAuditEventPerRoute(t *testing.T) {
	allowAllRoutes(t)

	mockDB := scopedMockDB(t)
	mockDB.On("GetFileStatusHistory", fileID).Return([]database.FileStatus{}, nil)
	mockDB.On("GetDatasetDetails", datasetID).Return(&database.DatasetDetails{Status: "registered"}, nil)
	mockDB.On("GetFileEvents").Return([]string{"disabled"}, nil)
	mockDB.On("UpdateFileEventLog", fileID, "disabled", userID, `{"reason":"test"}`, "{}").Return(nil)

	ingestBody := toJSON(t, map[string]any{"filepath": filePath, "user": userID})
	cancelBody := toJSON(t, map[string]any{"filepath": filePath, "fileid": fileID, "user": userID})
	accessionBody := toJSON(t, map[string]any{"accession_id": accessionID, "filepath": filePath, "user": userID})
	datasetBody := toJSON(t, dataset{AccessionIDs: []string{datasetID}, DatasetID: datasetID})
	c4ghBody := toJSON(t, map[string]any{"pubkey": publicKey, "description": "test c4gh public key"})
	reasonBody := toJSON(t, map[string]any{"reason": "test"})

	// One entry per route behind rbac in routes.go.
	tests := []struct {
		method, target string
		body           []byte
		wantCode       int
	}{
		{http.MethodGet, "/files", nil, http.StatusOK},
		{http.MethodPost, "/file/ingest", ingestBody, http.StatusOK},
		{http.MethodPost, "/file/cancel", cancelBody, http.StatusOK},
		{http.MethodPost, "/file/accession", accessionBody, http.StatusOK},
		{http.MethodPost, "/file/rotatekey/" + fileID, nil, http.StatusOK},
		{http.MethodGet, "/file/events/" + fileID, nil, http.StatusOK},
		{http.MethodPost, "/file/events/" + fileID + "/disabled", reasonBody, http.StatusOK},
		{http.MethodPut, "/file/verify/" + fileID, nil, http.StatusOK},
		{http.MethodDelete, "/file/" + userID + "/" + fileID, nil, http.StatusOK},
		{http.MethodGet, "/datasets", nil, http.StatusOK},
		{http.MethodGet, "/dataset/" + datasetID, nil, http.StatusOK},
		{http.MethodGet, "/datasets/list", nil, http.StatusOK},
		{http.MethodGet, "/datasets/list/" + userID, nil, http.StatusOK},
		{http.MethodPost, "/dataset/create", datasetBody, http.StatusOK},
		{http.MethodPost, "/dataset/rotatekey/" + datasetID, nil, http.StatusOK},
		{http.MethodPost, "/dataset/release/" + datasetID, nil, http.StatusOK},
		{http.MethodPut, "/dataset/verify/" + datasetID, nil, http.StatusOK},
		{http.MethodGet, "/users", nil, http.StatusOK},
		{http.MethodGet, "/users/" + userID + "/files?limit=1000", nil, http.StatusOK},
		// The success path needs a reencrypt gRPC server and a real crypt4gh header; the audit event is the same either way.
		{http.MethodGet, "/users/" + userID + "/file/" + fileID, nil, http.StatusBadRequest},
		{http.MethodGet, "/c4gh-keys/list", nil, http.StatusOK},
		{http.MethodPost, "/c4gh-keys/add", c4ghBody, http.StatusOK},
		{http.MethodPost, "/c4gh-keys/deprecate/somehash", nil, http.StatusOK},
	}
	handler := api.routes()
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			buf := captureAudit(t)
			r, w := newRequest(t, tc.method, tc.target, tc.body, token)
			handler.ServeHTTP(w, r)

			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			assert.NotContains(t, buf.String(), token, "the token must never reach the audit log")
			lines := auditLines(t, buf)
			require.Len(t, lines, 1, "exactly one audit event per request")
			ev := lines[0]
			wantPath, _, _ := strings.Cut(tc.target, "?")
			assert.Equal(t, "audit", ev["type"])
			assert.Equal(t, string(audit.EventAPIRequest), ev["event"])
			assert.Equal(t, tc.method, ev["method"])
			assert.Equal(t, wantPath, ev["path"], "path without the query")
			assert.Equal(t, "dummy", ev["userId"])
			assert.Equal(t, float64(w.Code), ev["httpStatus"], "audit status must match the response")
		})
	}
}

func TestAuditEventOmitsQuery(t *testing.T) {
	buf := captureAudit(t)
	r, w := newRequest(t, http.MethodGet, "/files?access_token=secret", nil, token)
	api.routes().ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, buf.String(), "secret")
	lines := auditLines(t, buf)
	require.Len(t, lines, 1)
	assert.Equal(t, "/files", lines[0]["path"])
}

func TestAuditEventWithoutToken(t *testing.T) {
	buf := captureAudit(t)
	r, w := newRequest(t, http.MethodGet, "/files", nil, "")
	api.routes().ServeHTTP(w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	lines := auditLines(t, buf)
	require.Len(t, lines, 1)
	assert.Equal(t, "", lines[0]["userId"])
	assert.Equal(t, float64(http.StatusUnauthorized), lines[0]["httpStatus"])
	assert.Equal(t, "/files", lines[0]["path"])
}

func TestAuditEventFileIDQuery(t *testing.T) {
	scopedMockDB(t).On("GetFileDetails", unknownID, "uploaded").Return((*database.FileDetails)(nil), sql.ErrNoRows)

	tests := []struct {
		target   string
		fileID   string
		wantCode int
	}{
		{"/file/cancel?fileid=" + fileID, fileID, http.StatusOK},
		// 4.0.x answers 400 for an unknown file where main answers 404; the fileId must still reach the event.
		{"/file/ingest?fileid=" + unknownID, unknownID, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			buf := captureAudit(t)
			r, w := newRequest(t, http.MethodPost, tc.target, nil, token)
			api.routes().ServeHTTP(w, r)

			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			lines := auditLines(t, buf)
			require.Len(t, lines, 1)
			assert.Equal(t, tc.fileID, lines[0]["fileId"])
		})
	}
}

func TestAuditEventFileIDTarget(t *testing.T) {
	allowAllRoutes(t)
	cancelBody := toJSON(t, map[string]any{"filepath": filePath, "user": userID})
	tests := []struct {
		name, method, target string
		body                 []byte
		want                 map[string]any
	}{
		{"cancel by body names the resolved file", http.MethodPost, "/file/cancel", cancelBody,
			map[string]any{"fileId": fileID, "filePath": filePath, "submissionUser": userID}},
		{"path id wins over a query fileid", http.MethodDelete, "/file/" + userID + "/" + fileID + "?fileid=" + unknownID, nil,
			map[string]any{"fileId": fileID}},
		{"query fileid ignored where the handler does not read it", http.MethodGet, "/files?fileid=" + unknownID, nil,
			map[string]any{"fileId": nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureAudit(t)
			r, w := newRequest(t, tc.method, tc.target, tc.body, token)
			api.routes().ServeHTTP(w, r)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			lines := auditLines(t, buf)
			require.Len(t, lines, 1)
			for k, v := range tc.want {
				assert.Equal(t, v, lines[0][k], k)
			}
		})
	}
}

func TestAuditEventStatus(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		want        int
		wantFlushed bool
	}{
		{"panic before writing", func(http.ResponseWriter, *http.Request) { panic("boom") }, http.StatusInternalServerError, false},
		{"panic after writing", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("accepted"))
			panic("boom")
		}, http.StatusAccepted, false},
		{"flush commits 200", func(w http.ResponseWriter, _ *http.Request) {
			_ = http.NewResponseController(w).Flush()
			w.WriteHeader(http.StatusServiceUnavailable)
		}, http.StatusOK, true},
		{"1xx is not final", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusCreated)
		}, http.StatusCreated, false},
		{"101 is final", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusSwitchingProtocols)
		}, http.StatusSwitchingProtocols, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureAudit(t)
			r, w := newRequest(t, http.MethodGet, "/files", nil, token)
			api.recoveryMiddleware(api.rbac(tc.handler)).ServeHTTP(w, r)

			lines := auditLines(t, buf)
			require.Len(t, lines, 1)
			assert.Equal(t, float64(tc.want), lines[0]["httpStatus"])
			assert.Equal(t, tc.wantFlushed, w.Flushed, "flush must reach the underlying writer")
		})
	}
}

func TestAuditEventBodyIdentifiers(t *testing.T) {
	tests := []struct {
		target string
		body   []byte
		want   map[string]any
	}{
		{
			"/file/ingest",
			toJSON(t, map[string]any{"filepath": filePath, "user": userID}),
			map[string]any{"fileId": fileID, "filePath": filePath, "submissionUser": userID},
		},
		{
			"/file/accession",
			toJSON(t, map[string]any{"accession_id": accessionID, "filepath": filePath, "user": userID}),
			map[string]any{"fileId": fileID, "filePath": filePath, "accessionId": accessionID},
		},
		{
			"/dataset/create",
			toJSON(t, dataset{AccessionIDs: []string{datasetID}, DatasetID: datasetID}),
			map[string]any{"datasetId": datasetID},
		},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			buf := captureAudit(t)
			r, w := newRequest(t, http.MethodPost, tc.target, tc.body, token)
			api.routes().ServeHTTP(w, r)

			require.Equal(t, http.StatusOK, w.Code)
			lines := auditLines(t, buf)
			require.Len(t, lines, 1)
			for k, v := range tc.want {
				assert.Equal(t, v, lines[0][k], k)
			}
			for _, k := range []string{"fileId", "filePath", "submissionUser", "accessionId", "datasetId"} {
				if _, ok := tc.want[k]; !ok {
					assert.NotContains(t, lines[0], k)
				}
			}
		})
	}
}

func TestLoggingMiddlewareStatus(t *testing.T) {
	prevLog := slog.Default()
	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLog) })

	handler := api.routes()
	requestLine := func(t *testing.T) map[string]any {
		t.Helper()
		for line := range strings.Lines(logBuf.String()) {
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &m))
			if m["msg"] == "request" {
				return m
			}
		}
		t.Fatal("no request log line")

		return nil
	}

	t.Run("not found", func(t *testing.T) {
		logBuf.Reset()
		r, w := newRequest(t, http.MethodGet, "/no/such/route", nil, "")
		handler.ServeHTTP(w, r)

		require.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, float64(http.StatusNotFound), requestLine(t)["status_code"])
	})

	t.Run("audit correlationId is set", func(t *testing.T) {
		buf := captureAudit(t)
		r, w := newRequest(t, http.MethodGet, "/files", nil, token)
		handler.ServeHTTP(w, r)

		require.Equal(t, http.StatusOK, w.Code)
		lines := auditLines(t, buf)
		require.Len(t, lines, 1)
		assert.NotEmpty(t, lines[0]["correlationId"])
	})
}
