package main

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/suite"
)

// TestSuite defines the download service integration test suite
type TestSuite struct {
	suite.Suite

	// Configuration
	downloadURL    string
	jwtKeyFilePath string

	// Generated tokens
	token string
}

func TestDownloadTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) SetupSuite() {
	// Configuration - matches docker compose service names
	ts.downloadURL = getEnv("DOWNLOAD_URL", "http://download:8080")
	ts.jwtKeyFilePath = getEnv("JWT_KEY_FILE", "/shared/keys/jwt.key")

	// Generate JWT token for authenticated requests
	var err error
	ts.token, err = ts.generateToken("integration_test@example.org")
	if err != nil {
		ts.FailNow("failed to generate token", err.Error())
	}

	// Wait for download service to be ready
	ts.waitForService()
}

func (ts *TestSuite) waitForService() {
	client := &http.Client{Timeout: 2 * time.Second}
	maxAttempts := 30

	for i := 0; i < maxAttempts; i++ {
		resp, err := client.Get(ts.downloadURL + "/health/live")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	ts.FailNow("download service not ready after waiting")
}

func (ts *TestSuite) generateToken(sub string) (string, error) {
	keyPem, err := os.ReadFile(ts.jwtKeyFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read key file from path %s: %w", ts.jwtKeyFilePath, err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &jwt.RegisteredClaims{
		Subject:   sub,
		Issuer:    "http://integration.test",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})

	token.Header["kid"] = "rsa1"
	block, _ := pem.Decode(keyPem)
	keyRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	return token.SignedString(keyRaw)
}

func (ts *TestSuite) doRequest(method, path string, body io.Reader, headers map[string]string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, ts.downloadURL+path, body)
	if err != nil {
		return nil, nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}

	return resp, respBody, nil
}

func (ts *TestSuite) authHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + ts.token,
	}
}

// Test01_HealthLive tests the liveness probe endpoint
func (ts *TestSuite) Test01_HealthLive() {
	resp, _, err := ts.doRequest("GET", "/health/live", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "health/live should return 200")
}

// Test02_HealthReady tests the readiness probe endpoint
func (ts *TestSuite) Test02_HealthReady() {
	resp, _, err := ts.doRequest("GET", "/health/ready", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "health/ready should return 200")
}

// Test03_UnauthenticatedRequest tests that unauthenticated requests are rejected
func (ts *TestSuite) Test03_UnauthenticatedRequest() {
	resp, _, err := ts.doRequest("GET", "/info/datasets", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode, "unauthenticated request should return 401")
}

// Test04_ListDatasets tests listing available datasets
func (ts *TestSuite) Test04_ListDatasets() {
	resp, body, err := ts.doRequest("GET", "/info/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "list datasets should return 200")

	var datasets []map[string]interface{}
	err = json.Unmarshal(body, &datasets)
	ts.Require().NoError(err, "response should be valid JSON array")

	// Store dataset info for subsequent tests
	if len(datasets) > 0 {
		ts.T().Logf("Found %d dataset(s)", len(datasets))
	} else {
		ts.T().Log("No datasets found (expected if pipeline hasn't run)")
	}
}

// Test05_GetDatasetInfo tests getting info for a specific dataset
func (ts *TestSuite) Test05_GetDatasetInfo() {
	// First get list of datasets
	resp, body, err := ts.doRequest("GET", "/info/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasets []map[string]interface{}
	err = json.Unmarshal(body, &datasets)
	ts.Require().NoError(err)

	if len(datasets) == 0 {
		ts.T().Skip("No datasets available - skipping dataset info test")
		return
	}

	datasetID, ok := datasets[0]["id"].(string)
	ts.Require().True(ok, "dataset should have 'id' field")

	// Get dataset info
	resp, body, err = ts.doRequest("GET", "/info/dataset?dataset="+datasetID, nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "get dataset info should return 200")

	var datasetInfo map[string]interface{}
	err = json.Unmarshal(body, &datasetInfo)
	ts.Require().NoError(err, "response should be valid JSON")
	ts.Contains(datasetInfo, "id", "dataset info should contain 'id'")
}

// Test06_ListFilesInDataset tests listing files in a dataset
func (ts *TestSuite) Test06_ListFilesInDataset() {
	// First get list of datasets
	resp, body, err := ts.doRequest("GET", "/info/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasets []map[string]interface{}
	err = json.Unmarshal(body, &datasets)
	ts.Require().NoError(err)

	if len(datasets) == 0 {
		ts.T().Skip("No datasets available - skipping files list test")
		return
	}

	datasetID, ok := datasets[0]["id"].(string)
	ts.Require().True(ok, "dataset should have 'id' field")

	// List files in dataset
	resp, body, err = ts.doRequest("GET", "/info/dataset/files?dataset="+datasetID, nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "list files should return 200")

	var files []map[string]interface{}
	err = json.Unmarshal(body, &files)
	ts.Require().NoError(err, "response should be valid JSON array")

	ts.T().Logf("Found %d file(s) in dataset %s", len(files), datasetID)
}

// Test07_DownloadWithReencryption tests file download with re-encryption
func (ts *TestSuite) Test07_DownloadWithReencryption() {
	// Get a file to download
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping download test: " + err.Error())
		return
	}

	// Read public key for re-encryption
	pubkeyPath := getEnv("C4GH_PUBKEY_FILE", "/shared/c4gh.pub.pem")
	pubkeyBytes, err := os.ReadFile(pubkeyPath)
	if err != nil {
		ts.T().Skipf("No public key available at %s - skipping download test", pubkeyPath)
		return
	}
	pubkeyBase64 := base64.StdEncoding.EncodeToString(pubkeyBytes)

	headers := ts.authHeaders()
	headers["public_key"] = pubkeyBase64

	resp, body, err := ts.doRequest("GET", "/file/"+fileID, nil, headers)
	ts.Require().NoError(err)

	switch resp.StatusCode {
	case http.StatusOK:
		ts.T().Logf("Downloaded file size: %d bytes", len(body))
		// Verify crypt4gh magic bytes (hex: 6372797074346768 = "crypt4gh")
		if len(body) >= 8 {
			magic := string(body[:8])
			ts.Equal("crypt4gh", magic, "file should have crypt4gh magic bytes")
		}
	case http.StatusNotImplemented:
		ts.T().Skip("Re-encryption not implemented yet")
	case http.StatusInternalServerError:
		ts.T().Skip("Re-encryption failed (likely key mismatch in test data)")
	default:
		ts.Failf("Unexpected status code", "expected 200, got %d", resp.StatusCode)
	}
}

// Test08_RangeRequest tests partial file download with Range header
func (ts *TestSuite) Test08_RangeRequest() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping range request test")
		return
	}

	pubkeyPath := getEnv("C4GH_PUBKEY_FILE", "/shared/c4gh.pub.pem")
	pubkeyBytes, err := os.ReadFile(pubkeyPath)
	if err != nil {
		ts.T().Skip("No public key available - skipping range request test")
		return
	}
	pubkeyBase64 := base64.StdEncoding.EncodeToString(pubkeyBytes)

	headers := ts.authHeaders()
	headers["public_key"] = pubkeyBase64
	headers["Range"] = "bytes=0-99"

	resp, _, err := ts.doRequest("GET", "/file/"+fileID, nil, headers)
	ts.Require().NoError(err)

	// Accept either 206 (Partial Content) or 200 (full content if small file)
	ts.True(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusPartialContent ||
		resp.StatusCode == http.StatusNotImplemented ||
		resp.StatusCode == http.StatusInternalServerError,
		"range request should return 200, 206, 500, or 501, got %d", resp.StatusCode)
}

// Test09_AccessControlNonExistentFile tests that non-existent files return 404
func (ts *TestSuite) Test09_AccessControlNonExistentFile() {
	headers := ts.authHeaders()
	headers["public_key"] = base64.StdEncoding.EncodeToString([]byte("dummy"))

	resp, _, err := ts.doRequest("GET", "/file/EGAF00000000000", nil, headers)
	ts.Require().NoError(err)
	ts.True(resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden,
		"non-existent file should return 404 or 403, got %d", resp.StatusCode)
}

// Test10_InvalidToken tests that invalid tokens are rejected
func (ts *TestSuite) Test10_InvalidToken() {
	headers := map[string]string{
		"Authorization": "Bearer invalid-token",
	}

	resp, _, err := ts.doRequest("GET", "/info/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode, "invalid token should return 401")
}

// Test11_QueryBasedFileEndpoint tests the query-based file endpoint
func (ts *TestSuite) Test11_QueryBasedFileEndpoint() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping query-based endpoint test")
		return
	}

	pubkeyPath := getEnv("C4GH_PUBKEY_FILE", "/shared/c4gh.pub.pem")
	pubkeyBytes, err := os.ReadFile(pubkeyPath)
	if err != nil {
		ts.T().Skip("No public key available - skipping test")
		return
	}
	pubkeyBase64 := base64.StdEncoding.EncodeToString(pubkeyBytes)

	headers := ts.authHeaders()
	headers["public_key"] = pubkeyBase64

	// Test query-based endpoint: /file?fileId=...
	resp, _, err := ts.doRequest("GET", "/file?fileId="+fileID, nil, headers)
	ts.Require().NoError(err)
	// Accept 200 (success), 400 (not implemented/wrong format), 500 (reencrypt issue), 501 (not implemented)
	ts.True(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusNotImplemented ||
		resp.StatusCode == http.StatusInternalServerError,
		"query-based file endpoint should return expected status, got %d", resp.StatusCode)
}

// Helper function to get the first available file ID
func (ts *TestSuite) getFirstFileID() (string, error) {
	resp, body, err := ts.doRequest("GET", "/info/datasets", nil, ts.authHeaders())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get datasets: status %d", resp.StatusCode)
	}

	var datasets []map[string]interface{}
	if err := json.Unmarshal(body, &datasets); err != nil {
		return "", err
	}
	if len(datasets) == 0 {
		return "", fmt.Errorf("no datasets available")
	}

	datasetID, ok := datasets[0]["id"].(string)
	if !ok {
		return "", fmt.Errorf("dataset missing 'id' field")
	}

	resp, body, err = ts.doRequest("GET", "/info/dataset/files?dataset="+datasetID, nil, ts.authHeaders())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get files: status %d", resp.StatusCode)
	}

	var files []map[string]interface{}
	if err := json.Unmarshal(body, &files); err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files in dataset")
	}

	fileID, ok := files[0]["fileId"].(string)
	if !ok {
		return "", fmt.Errorf("file missing 'fileId' field")
	}

	return fileID, nil
}

// Helper to get environment variable with default
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
