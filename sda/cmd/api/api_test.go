package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var (
	api               API
	token             string
	filePath          = "test/file.c4gh"
	fileID            = "3a7f2c91-b4e0-4d8a-9f3b-2c6e1a0d5f8e"
	userID            = "dummy"
	datasetID         = "test-dataset-123"
	accessionID       = "test-file-123"
	decryptedChecksum = "a3f1c2d4e5b6789012345678abcdef901234567890abcdef1234567890abcdef"
	datasetFileIDs    = []string{fileID}
	datasetFiles      = []string{fileID}
	userFiles         = []*database.SubmissionFileInfo{
		{AccessionID: accessionID, FileID: fileID},
	}
	userDatasets       = []*database.DatasetInfo{{DatasetID: datasetID, Status: "status", Timestamp: "time"}}
	reverificationData = &database.ReVerificationData{
		FileID:               fileID,
		ArchiveFilePath:      "archive",
		SubmissionFilePath:   "inbox",
		SubmissionUser:       userID,
		ArchivedCheckSum:     "a3f8c2d1e9b47056f2a1c8e3d5b690f4",
		ArchivedCheckSumType: "md5",
	}
	mappingData = &database.MappingData{FileID: fileID, User: userID, SubmissionFilePath: "inbox", SubmissionLocation: "archive"}
	pemBytes    []byte
	publicKey   string
)

func setup() error {
	viper.Set("database.host", "")
	viper.Set("database.user", "")
	viper.Set("database.password", "")
	if err := config.Load(); err != nil {
		return fmt.Errorf("failed to load config, reason: %v", err)
	}

	// Tests are executed relative to the packages directory, this properly sets the schema path so they can be accessed when running the test
	apiconfig.SetSchemaPath("../../schemas/isolated")

	jwkPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate rsa key pair, reason: %v", err)
	}

	jwkKey, err := jwk.FromRaw(jwkPrivateKey)
	if err != nil {
		return fmt.Errorf("faield to generate jwk key, reason: %v", err)
	}

	token, err = helper.CreateRSAToken(jwkKey, "RS256", helper.DefaultTokenClaims)
	if err != nil {
		return fmt.Errorf("failed to create rsa token, reason: %v", err)
	}

	jwkPublicKeyBytes, err := jwk.EncodePEM(&jwkPrivateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to pem encode jwk public key, reason: %v", err)
	}

	auth := userauth.NewValidateFromToken(jwk.NewSet())
	err = auth.ReadJwtPubKeyBytes(jwkPublicKeyBytes)
	if err != nil {
		return fmt.Errorf("failed to read jwk public key, reason: %v", err)
	}

	api.auth = auth

	pubBytes, _, err := keys.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate c4gh key pair, reason: %v", err)
	}

	pemBytes = pem.EncodeToMemory(&pem.Block{
		Type:  "CRYPT4GH PUBLIC KEY",
		Bytes: pubBytes[:],
	})

	publicKey = base64.StdEncoding.EncodeToString(pemBytes)

	var buf bytes.Buffer
	sha256hash := sha256.New()
	_ = io.MultiWriter(&buf, sha256hash)

	api.inboxReader = &mocks.MockReader{Data: buf.Bytes()}
	api.inboxWriter = &mocks.MockWriter{}

	rbac := []byte(
		`{"policy":[{"role":"admin","path":"/c4gh-keys/*","action":"(GET)|(POST)|(PUT)"},
		{"role":"submission","path":"/datasets","action":"GET"},
		{"role":"submission","path":"/datasets/list","action":"GET"},
		{"role":"submission","path":"/datasets/list/:user","action":"GET"},
		{"role":"submission","path":"/dataset/create","action":"POST"},
		{"role":"submission","path":"/dataset/rotatekey/:datasetid","action":"POST"},
		{"role":"submission","path":"/dataset/rotatekey/","action":"POST"},
		{"role":"submission","path":"/dataset/release/:datasetid","action":"POST"},
		{"role":"submission","path":"/dataset/verify/","action":"PUT"},
		{"role":"submission","path":"/file/ingest","action":"POST"},
		{"role":"submission","path":"/file/verify/","action":"PUT"},
		{"role":"submission","path":"/file/accession","action":"POST"},
		{"role":"submission","path":"/file","action":"DELETE"},
		{"role":"submission","path":"/file/rotatekey/:fileid","action":"POST"},
		{"role":"submission","path":"/users","action":"GET"},
		{"role":"submission","path":"/users/:username/files","action":"GET"},
		{"role":"submission","path":"/users/:username/file/:fileid","action":"GET"},
		{"role":"*","path":"/files","action":"GET"}],
		"roles":[{"role":"admin","rolebinding":"submission"},
		{"role":"dummy","rolebinding":"admin"}]}`)
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		return fmt.Errorf("failed to create jsonadapter model, reason: %v", err)
	}

	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&rbac))
	if err != nil {
		return fmt.Errorf("faield to create new casbin enforcer instance, reason: %v", err)
	}

	api.enforcer = e
	mockDB := new(mocks.MockDatabase)

	mockDB.On("Ping").Return(nil)
	mockDB.On("GetUserFiles", userID, "", false, 1000, "").Return(userFiles, "", nil)
	mockDB.On("GetUserFiles", userID, "", true, 1000, "").Return(userFiles, "", nil)
	mockDB.On("GetFileIDByUserPathAndStatus", userID, filePath, "uploaded").Return(fileID, nil)
	mockDB.On("GetFileIDByUserPathAndStatus", userID, filePath, "verified").Return(fileID, nil)
	mockDB.On("GetUploadedSubmissionFilePathAndLocation", userID, fileID).Return("inbox", "inbox", nil)
	mockDB.On("GetDatasetStatus", datasetID).Return("registered", nil)
	mockDB.On("GetDecryptedChecksum", fileID).Return(decryptedChecksum, nil)
	mockDB.On("GetDatasetFileIDs", datasetID).Return(datasetFileIDs, nil)
	mockDB.On("GetDatasetFiles", datasetID).Return(datasetFiles, nil)
	mockDB.On("GetReVerificationData", fileID).Return(reverificationData, nil)
	mockDB.On("GetMappingData", datasetID).Return(mappingData, nil)
	mockDB.On("ListUserDatasets", userID).Return(userDatasets, nil)
	mockDB.On("ListDatasets").Return(userDatasets, nil)
	mockDB.On("CheckIfDatasetExists", datasetID).Return(true, nil)
	mockDB.On("ListActiveUsers").Return([]string{userID}, nil)
	mockDB.On("ListKeyHashes").Return([]*database.C4ghKeyHash{{Hash: "", Description: "test", CreatedAt: "time", DeprecatedAt: "time"}}, nil)
	mockDB.On("AddKeyHash", mock.Anything, mock.Anything).Return(nil)
	mockDB.On("DeprecateKeyHash", mock.Anything).Return(nil)
	mockDB.On("UpdateFileEventLog", fileID, "disabled", "api", "{}", "{}").Return(nil)

	api.db = mockDB
	api.mq = &mocks.MockBroker{}

	return nil
}

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		log.Fatalf("setup failed, reason: %v", err)
	}
	os.Exit(m.Run())
}

func newRequest(t *testing.T, method, target string, body []byte, token string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var r *http.Request
	if len(body) > 0 {
		r = httptest.NewRequest(method, target, bytes.NewBuffer(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	return r, httptest.NewRecorder()
}

func toJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	assert.NoError(t, err)

	return b
}

func TestReadinessResponse(t *testing.T) {
	r, w := newRequest(t, http.MethodGet, "/ready", nil, "")
	api.readinessResponse(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetFiles(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"Valid Token", token, http.StatusOK},
		{"Invalid Token", "invalidtoken", http.StatusUnauthorized},
		{"Missing Token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/files", nil, tc.token)
			api.rbac(api.getFiles)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestIngestFile(t *testing.T) {
	validBody := toJSON(t, map[string]any{
		"filepath": filePath,
		"user":     userID,
	})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
		{"Invalid Body", token, []byte("not json"), http.StatusBadRequest},
		{"Empty Body", token, nil, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, "/file/ingest", tc.body, tc.token)
			api.rbac(api.ingestFile)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestSetAccession(t *testing.T) {
	validBody := toJSON(t, map[string]any{
		"accession_id": accessionID,
		"filepath":     filePath,
		"user":         userID,
	})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
		{"Invalid Body", token, []byte("not json"), http.StatusBadRequest},
		{"Empty Body", token, nil, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, "/file/accession", tc.body, tc.token)
			api.rbac(api.setAccession)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestRotateKeyFile(t *testing.T) {
	url := "/file/rotatekey/"
	tests := []struct {
		name     string
		token    string
		url      string
		fileID   string
		wantCode int
	}{
		{"Valid Request", token, url, fileID, http.StatusOK},
		{"Invalid Token", url, url, fileID, http.StatusUnauthorized},
		{"Missing Token", "", url, fileID, http.StatusUnauthorized},
		{"Invalid FileID", token, url, "invalidFileID", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, tc.url+tc.fileID, nil, tc.token)
			r.SetPathValue("fileid", tc.fileID)
			api.rbac(api.rotateKeyFile)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestDeleteFile(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		username string
		fileID   string
		wantCode int
	}{
		{"Valid Request", token, userID, fileID, http.StatusOK},
		{"Invalid Token", "invalidtoken", userID, fileID, http.StatusUnauthorized},
		{"Missing Token", "", userID, fileID, http.StatusUnauthorized},
		{"Missing FileID", token, userID, "", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodDelete, "/file", nil, tc.token)
			r.SetPathValue("username", tc.username)
			r.SetPathValue("fileid", tc.fileID)
			api.rbac(api.deleteFile)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListDatasets(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"Valid Token", token, http.StatusOK},
		{"Invalid Token", "invalidtoken", http.StatusUnauthorized},
		{"Missing Token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/datasets", nil, tc.token)
			api.rbac(api.listDatasets)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListAllDatasets(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"Valid Token", token, http.StatusOK},
		{"Invalid Token", "invalidtoken", http.StatusUnauthorized},
		{"Missing Token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/datasets/list", nil, tc.token)
			api.rbac(api.listAllDatasets)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListUserDatasets(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		username string
		wantCode int
	}{
		{"Valid Request", token, userID, http.StatusOK},
		{"Invalid Token", "invalidtoken", userID, http.StatusUnauthorized},
		{"Missing Token", "", userID, http.StatusUnauthorized},
		{"Missing Username", token, "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/datasets/list/"+tc.username, nil, tc.token)
			r.SetPathValue("username", tc.username)
			api.rbac(api.listUserDatasets)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestCreateDataset(t *testing.T) {
	validBody := toJSON(t, dataset{
		AccessionIDs: []string{datasetID},
		DatasetID:    datasetID,
	})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
		{"Invalid Body", token, []byte("not json"), http.StatusBadRequest},
		{"Empty Body", token, nil, http.StatusBadRequest},
		{
			name:     "Missing AccessionIDs",
			token:    token,
			body:     toJSON(t, dataset{DatasetID: datasetID}),
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, "/dataset/create", tc.body, tc.token)
			api.rbac(api.createDataset)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestRotateKeyDataset(t *testing.T) {
	url := "/dataset/rotatekey/"
	tests := []struct {
		name      string
		token     string
		url       string
		datasetID string
		wantCode  int
	}{
		{"Valid Request", token, url, datasetID, http.StatusOK},
		{"Invalid Token", "invalidtoken", url, datasetID, http.StatusUnauthorized},
		{"Missing Token", "", url, datasetID, http.StatusUnauthorized},
		{"Missing DatasetID", token, url, "", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, tc.url+tc.datasetID, nil, tc.token)
			r.SetPathValue("datasetid", tc.datasetID)
			api.rbac(api.rotateKeyDataset)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestReleaseDataset(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		url      string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, "/dataset/release/" + datasetID, []byte{}, http.StatusOK},
		{"Invalid Token", "invalidToken", "/dataset/release/" + datasetID, []byte{}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, tc.url, tc.body, tc.token)
			// since the request don't route through the mux we need to bind the path value here as well
			r.SetPathValue("datasetid", datasetID)
			api.rbac(api.releaseDataset)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestVerifyFile(t *testing.T) {
	validBody := toJSON(t, map[string]any{"dataset_id": datasetID})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPut, "/file/verify/", tc.body, tc.token)
			r.SetPathValue("accession", fileID)
			api.rbac(api.verifyFile)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestVerifyDataset(t *testing.T) {
	validBody := toJSON(t, map[string]any{"dataset_id": datasetID})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPut, "/dataset/verify/", tc.body, tc.token)
			r.SetPathValue("dataset", datasetID)
			api.rbac(api.verifyDataset)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListActiveUsers(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"Valid Token", token, http.StatusOK},
		{"Invalid Token", "invalidtoken", http.StatusUnauthorized},
		{"Missing Token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/users", nil, tc.token)
			api.rbac(api.listActiveUsers)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListUserFiles(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		username string
		wantCode int
	}{
		{"Valid Request", token, userID, http.StatusOK},
		{"Invalid Token", "invalidtoken", userID, http.StatusUnauthorized},
		{"Missing Token", "", userID, http.StatusUnauthorized},
		{"Missing Username", token, "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/users/"+tc.username+"/files", nil, tc.token)
			r.SetPathValue("username", tc.username)
			api.rbac(api.listUserFiles)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestDownloadFile(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		username string
		fileID   string
		wantCode int
	}{
		{"Invalid Token", "invalidtoken", userID, fileID, http.StatusUnauthorized},
		{"Missing Token", "", userID, fileID, http.StatusUnauthorized},
		{"Missing FileID", token, userID, "", http.StatusUnauthorized},
		{"Missing Username", token, "", fileID, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/users/"+tc.username+"/file/"+tc.fileID, nil, tc.token)
			r.SetPathValue("username", tc.username)
			r.SetPathValue("fileid", tc.fileID)
			r.Header.Add("C4GH-Public-Key", publicKey)
			api.rbac(api.downloadFile)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestListC4ghHashes(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"Valid Token", token, http.StatusOK},
		{"Invalid Token", "invalidtoken", http.StatusUnauthorized},
		{"Missing Token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodGet, "/c4gh-keys/list", nil, tc.token)
			api.rbac(api.listC4ghHashes)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestAddC4ghHash(t *testing.T) {
	validBody := toJSON(t, map[string]any{
		"pubkey":      base64.StdEncoding.EncodeToString(pemBytes),
		"description": "test c4gh public key",
	})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
		{"Invalid Body", token, []byte("not json"), http.StatusBadRequest},
		{"Empty Body", token, nil, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, "/c4gh-keys/add", tc.body, tc.token)
			api.rbac(api.addC4ghHash)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestDeprecateC4ghHash(t *testing.T) {
	validBody := toJSON(t, map[string]any{
		"pubkey": publicKey,
	})
	tests := []struct {
		name     string
		token    string
		body     []byte
		wantCode int
	}{
		{"Valid Request", token, validBody, http.StatusOK},
		{"Invalid Token", "invalidtoken", validBody, http.StatusUnauthorized},
		{"Missing Token", "", validBody, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, w := newRequest(t, http.MethodPost, "/c4gh-keys/deprecate/", tc.body, tc.token)
			api.rbac(api.deprecateC4ghHash)(w, r)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}
