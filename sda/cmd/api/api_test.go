package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	"github.com/neicnordic/sensitive-data-archive/internal/inboxpath"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
	content := buf.Bytes()

	mockReader := &mocks.MockReader{}
	mockReader.On("NewFileReader", mock.Anything, mock.Anything).Return(content, nil)
	mockReader.On("GetFileSize", mock.Anything, mock.Anything).Return(int64(len(content)), nil)

	api.inboxReader = mockReader

	mockWriter := &mocks.MockWriter{}
	mockWriter.On("RemoveFile", mock.Anything, mock.Anything).Return(nil)
	api.inboxWriter = mockWriter

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
	mockBroker := &mocks.MockBroker{}
	mockBroker.On("Publish", mock.Anything, mock.Anything).Return(nil)
	mockBroker.On("Alive").Return(true)
	api.mq = mockBroker

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

// setup runs configv2.Load, the only place in the unit suites where the real flag registration is
// exercised. Importing inboxproject is what registers the inbox keys, so if that ever stops
// happening the flags vanish from --help and no config file can switch the layout on. Nothing
// else fails when they are missing, since an unregistered key just reads as the stock default.
func TestInboxProjectFlagsAreRegistered(t *testing.T) {
	// AllKeys includes keys bound from pflags, which is what registration produces; IsSet does not
	// consider a flag's default, so an unset string flag would read as absent either way.
	registered := map[string]bool{}
	for _, key := range viper.AllKeys() {
		registered[key] = true
	}
	for _, key := range []string{"inboxpath.project_code", "inboxpath.project_delimiter"} {
		assert.True(t, registered[key], "%s should be bound after config load, got keys: %v", key, viper.AllKeys())
	}
}

// submissionUser carries an "@" on purpose: the stock branch normalizes it to "_" while the
// project-code branch keeps it raw, so the two expectations in each table differ by the username
// itself and not only by the prefix.
const submissionUser = "dummy@elixir-europe.org"

const projectCodeConfig = "inboxpath:\n  project_code: p11\n  project_delimiter: \"-\"\n"

// useInboxLayout installs an inbox layout through the package's real entry point, so these tests
// exercise the same path a service takes at startup.
func useInboxLayout(t *testing.T, yaml string) {
	t.Helper()
	t.Cleanup(func() {
		viper.Reset()
		require.NoError(t, inboxpath.Load())
	})

	viper.Reset()
	if yaml != "" {
		viper.SetConfigType("yaml")
		require.NoError(t, viper.ReadConfig(bytes.NewBufferString(yaml)))
	}
	require.NoError(t, inboxpath.Load())
}

func TestDeleteFile_resolvesInboxProjectPath(t *testing.T) {
	// A deployment with a project code stores the inbox directory as "<code><delimiter><rawuser>",
	// so the stored anonymized path must be resolved against that layout before the inbox write.
	// Resolving it as the stock normalized username instead targets a directory that does not
	// exist and the delete silently misses.
	for _, tc := range []struct {
		name     string
		yaml     string
		wantPath string
	}{
		{"stock", "", "dummy_elixir-europe.org/files/x.c4gh"},
		{"project code", projectCodeConfig, "p11-dummy@elixir-europe.org/files/x.c4gh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDB := &mocks.MockDatabase{}
			mockDB.On("GetUploadedSubmissionFilePathAndLocation", submissionUser, fileID).
				Return("files/x.c4gh", "inbox-location", nil).Once()
			mockDB.On("UpdateFileEventLog", fileID, "disabled", "api", "{}", "{}").Return(nil).Once()

			mockWriter := &mocks.MockWriter{}
			mockWriter.On("RemoveFile", "inbox-location", tc.wantPath).Return(nil).Once()

			useInboxLayout(t, tc.yaml)
			apiImpl := &API{db: mockDB, inboxWriter: mockWriter}

			r, w := newRequest(t, http.MethodDelete, "/file", nil, "")
			r.SetPathValue("username", submissionUser)
			r.SetPathValue("fileid", fileID)
			apiImpl.deleteFile(w, r)

			assert.Equal(t, http.StatusOK, w.Code)
			mockDB.AssertExpectations(t)
			mockWriter.AssertExpectations(t)
		})
	}
}

func TestDownloadFile_resolvesInboxProjectPath(t *testing.T) {
	// Same layout contract on the read side. The reader is failed deliberately: the assertion is
	// the path it was handed, not the crypt4gh stream that would follow.
	for _, tc := range []struct {
		name     string
		yaml     string
		wantPath string
	}{
		{"stock", "", "dummy_elixir-europe.org/files/x.c4gh"},
		{"project code", projectCodeConfig, "p11-dummy@elixir-europe.org/files/x.c4gh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDB := &mocks.MockDatabase{}
			mockDB.On("GetUploadedSubmissionFilePathAndLocation", submissionUser, fileID).
				Return("files/x.c4gh", "inbox-location", nil).Once()

			mockReader := &mocks.MockReader{}
			mockReader.On("NewFileReader", "inbox-location", tc.wantPath).
				Return(nil, errors.New("read failed")).Once()

			useInboxLayout(t, tc.yaml)
			apiImpl := &API{db: mockDB, inboxReader: mockReader}

			r, w := newRequest(t, http.MethodGet, "/users/file", nil, "")
			r.Header.Set("C4GH-Public-Key", base64.StdEncoding.EncodeToString([]byte(publicKey)))
			r.SetPathValue("username", submissionUser)
			r.SetPathValue("fileid", fileID)
			apiImpl.downloadFile(w, r)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			mockDB.AssertExpectations(t)
			mockReader.AssertExpectations(t)
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
func TestGetDatasetDetails(t *testing.T) {
	for _, tc := range []struct {
		name               string
		datasetID          string
		newMockDB          func() *mocks.MockDatabase
		assertMockDB       func(*testing.T, *mocks.MockDatabase)
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name:      "not_found",
			datasetID: "not_found",
			newMockDB: func() *mocks.MockDatabase {
				mockDB := &mocks.MockDatabase{}
				mockDB.On("GetDatasetDetails", "not_found").Return(nil, nil).Once()

				return mockDB
			},
			assertMockDB: func(t *testing.T, mockDB *mocks.MockDatabase) {
				mockDB.AssertExpectations(t)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBody:       fmt.Sprintf(`{"error": "%s"}`, http.StatusText(http.StatusNotFound)),
		},
		{
			name:      "internal_db_error",
			datasetID: "internal_db_error",
			newMockDB: func() *mocks.MockDatabase {
				mockDB := &mocks.MockDatabase{}
				mockDB.On("GetDatasetDetails", "internal_db_error").Return(nil, errors.New("internal db error")).Once()

				return mockDB
			},
			assertMockDB: func(t *testing.T, mockDB *mocks.MockDatabase) {
				mockDB.AssertExpectations(t)
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       fmt.Sprintf(`{"error": "%s"}`, http.StatusText(http.StatusInternalServerError)),
		}, {
			name:      "found",
			datasetID: "found",
			newMockDB: func() *mocks.MockDatabase {
				mockDB := &mocks.MockDatabase{}
				mockDB.On("GetDatasetDetails", "found").Return(&database.DatasetDetails{
					Status:        "registered",
					CreatedAt:     "2026-08-19 09:25:29.477540 +00:00",
					NumberOfFiles: 1234,
				}, nil).Once()

				return mockDB
			},
			assertMockDB: func(t *testing.T, mockDB *mocks.MockDatabase) {
				mockDB.AssertExpectations(t)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       "{\"status\":\"registered\",\"createdAt\":\"2026-08-19 09:25:29.477540 +00:00\",\"numberOfFiles\":1234}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDB := tc.newMockDB()

			apiImpl := &API{
				db: mockDB,
			}

			mux := http.NewServeMux()
			mux.HandleFunc("GET /dataset/{datasetid}", apiImpl.getDataset)

			r, w := newRequest(t, http.MethodGet, "/dataset/"+tc.datasetID, nil, "")
			mux.ServeHTTP(w, r)

			body, err := io.ReadAll(w.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, tc.expectedBody, string(body))
			assert.Equal(t, tc.expectedStatusCode, w.Code)

			tc.assertMockDB(t, mockDB)
		})
	}
}
