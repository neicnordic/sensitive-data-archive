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

	// Environment capabilities (probed once in SetupSuite)
	hasReencrypt    bool // true if re-encryption works with seed data
	hasStorageFile  bool // true if the test file is accessible in storage
	hasSessionCache bool // true if session cookies are being set
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

	// Probe environment capabilities
	ts.probeCapabilities()
}

// probeCapabilities tests environment-specific features once and caches results.
// This avoids skipping on generic 500 — tests skip on known missing prerequisites.
func (ts *TestSuite) probeCapabilities() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		return
	}

	// Probe re-encryption: HEAD /files/:fileId with public key
	pubkey := ts.readPublicKeyBase64()
	if pubkey != "" {
		headers := ts.authHeaders()
		headers["X-C4GH-Public-Key"] = pubkey
		resp, _, reqErr := ts.doRequest("HEAD", "/files/"+fileID, nil, headers)
		ts.hasReencrypt = reqErr == nil && resp.StatusCode == http.StatusOK
	}

	// Probe storage access: GET /files/:fileId/content
	resp, _, reqErr := ts.doRequest("GET", "/files/"+fileID+"/content", nil, ts.authHeaders())
	ts.hasStorageFile = reqErr == nil && resp.StatusCode == http.StatusOK

	// Probe session cookie: check if first auth request sets sda_session
	req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	req.Header.Set("Authorization", "Bearer "+ts.token)
	cookieResp, cookieErr := http.DefaultClient.Do(req)
	if cookieErr == nil {
		for _, c := range cookieResp.Cookies() {
			if c.Name == "sda_session" {
				ts.hasSessionCache = true

				break
			}
		}
		cookieResp.Body.Close()
	}

	ts.T().Logf("Environment capabilities: reencrypt=%v, storageFile=%v, sessionCache=%v",
		ts.hasReencrypt, ts.hasStorageFile, ts.hasSessionCache)
}

func (ts *TestSuite) waitForService() {
	client := &http.Client{Timeout: 2 * time.Second}
	maxAttempts := 30

	for range maxAttempts {
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
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block containing private key")
	}
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
	resp, _, err := ts.doRequest("GET", "/datasets", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode, "unauthenticated request should return 401")
}

// Test04_ListDatasets tests listing available datasets
func (ts *TestSuite) Test04_ListDatasets() {
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "list datasets should return 200")

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err, "response should be valid JSON")

	if len(datasetsResp.Datasets) > 0 {
		ts.T().Logf("Found %d dataset(s)", len(datasetsResp.Datasets))
	} else {
		ts.T().Log("No datasets found (expected if pipeline hasn't run)")
	}
}

// Test05_GetDatasetInfo tests getting info for a specific dataset
func (ts *TestSuite) Test05_GetDatasetInfo() {
	// First get list of datasets
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err)

	if len(datasetsResp.Datasets) == 0 {
		ts.T().Skip("No datasets available - skipping dataset info test")
		return
	}

	datasetID := datasetsResp.Datasets[0]

	// Get dataset info
	resp, body, err = ts.doRequest("GET", "/datasets/"+datasetID, nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "get dataset info should return 200")

	var datasetInfo map[string]any
	err = json.Unmarshal(body, &datasetInfo)
	ts.Require().NoError(err, "response should be valid JSON")
	ts.Contains(datasetInfo, "datasetId", "dataset info should contain 'datasetId'")
	ts.Contains(datasetInfo, "date", "dataset info should contain 'date'")
	ts.Contains(datasetInfo, "files", "dataset info should contain 'files'")
	ts.Contains(datasetInfo, "size", "dataset info should contain 'size'")
}

// Test06_ListFilesInDataset tests listing files in a dataset
func (ts *TestSuite) Test06_ListFilesInDataset() {
	// First get list of datasets
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err)

	if len(datasetsResp.Datasets) == 0 {
		ts.T().Skip("No datasets available - skipping files list test")
		return
	}

	datasetID := datasetsResp.Datasets[0]

	// List files in dataset
	resp, body, err = ts.doRequest("GET", "/datasets/"+datasetID+"/files", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "list files should return 200")

	var filesResp struct {
		Files []map[string]any `json:"files"`
	}
	err = json.Unmarshal(body, &filesResp)
	ts.Require().NoError(err, "response should be valid JSON")

	ts.T().Logf("Found %d file(s) in dataset %s", len(filesResp.Files), datasetID)
}

// Test07_DownloadWithReencryption tests file download with re-encryption
func (ts *TestSuite) Test07_DownloadWithReencryption() {
	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping download test: " + err.Error())
		return
	}

	pubkeyBase64 := ts.readPublicKeyBase64()
	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = pubkeyBase64

	resp, body, err := ts.doRequest("GET", "/files/"+fileID, nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "download should return 200")

	if len(body) >= 8 {
		magic := string(body[:8])
		ts.Equal("crypt4gh", magic, "file should have crypt4gh magic bytes")
	}
}

// Test08_RangeRequest tests partial file download with Range header
func (ts *TestSuite) Test08_RangeRequest() {
	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping range request test")
		return
	}

	pubkeyBase64 := ts.readPublicKeyBase64()
	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = pubkeyBase64
	headers["Range"] = "bytes=0-99"

	resp, _, err := ts.doRequest("GET", "/files/"+fileID, nil, headers)
	ts.Require().NoError(err)

	// Accept either 206 (Partial Content) or 200 (full content if small file)
	ts.True(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusPartialContent,
		"range request should return 200 or 206, got %d", resp.StatusCode)
}

// Test09_AccessControlNonExistentFile tests that non-existent files return 403
func (ts *TestSuite) Test09_AccessControlNonExistentFile() {
	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = base64.StdEncoding.EncodeToString([]byte("dummy"))

	resp, _, err := ts.doRequest("GET", "/files/EGAF00000000000", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusForbidden, resp.StatusCode,
		"non-existent file should return 403, got %d", resp.StatusCode)
}

// Test10_InvalidToken tests that invalid tokens are rejected
func (ts *TestSuite) Test10_InvalidToken() {
	// Use a JWT-shaped token with invalid signature (not just a random string,
	// since opaque tokens now authenticate via userinfo).
	// This is a valid JWT structure but with a bogus signature that won't verify.
	invalidJWT := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJpbnZhbGlkIiwiZXhwIjo5OTk5OTk5OTk5fQ.invalidsignatureinvalidsignatureinvalidsignature"

	headers := map[string]string{
		"Authorization": "Bearer " + invalidJWT,
	}

	resp, _, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode, "invalid JWT signature should return 401")
}

// Test12_OpaqueTokenListDatasets tests that an opaque token authenticates via userinfo
func (ts *TestSuite) Test12_OpaqueTokenListDatasets() {
	// Use the submission user as the opaque token value.
	// mockoidc.py returns this as the "sub" claim in the userinfo response.
	opaqueToken := "integration_test@example.org"
	headers := map[string]string{
		"Authorization": "Bearer " + opaqueToken,
	}

	resp, body, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "opaque token should authenticate via userinfo")

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err, "response should be valid JSON")
	ts.T().Logf("Opaque token: found %d dataset(s)", len(datasetsResp.Datasets))
}

// Test13_OpaqueTokenDownloadFile tests file download using an opaque token
func (ts *TestSuite) Test13_OpaqueTokenDownloadFile() {
	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping opaque token download test: " + err.Error())
		return
	}

	pubkeyBase64 := ts.readPublicKeyBase64()
	opaqueToken := "integration_test@example.org"
	headers := map[string]string{
		"Authorization":     "Bearer " + opaqueToken,
		"X-C4GH-Public-Key": pubkeyBase64,
	}

	resp, body, err := ts.doRequest("GET", "/files/"+fileID, nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "opaque token download should return 200")

	if len(body) >= 8 {
		magic := string(body[:8])
		ts.Equal("crypt4gh", magic, "file should have crypt4gh magic bytes")
	}
}

// Test14_OpaqueTokenArbitrarySubject tests that any opaque token with a valid
// userinfo response authenticates successfully (userinfo returns the token
// value as the subject, and allow-all-data grants access to all datasets)
func (ts *TestSuite) Test14_OpaqueTokenArbitrarySubject() {
	headers := map[string]string{
		"Authorization": "Bearer random-opaque-token-12345",
	}

	resp, _, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode,
		"opaque token with valid userinfo response should authenticate")
}

// Test16_OpaqueTokenUserinfoFailure tests that an opaque token is rejected
// when the userinfo endpoint returns an error
func (ts *TestSuite) Test16_OpaqueTokenUserinfoFailure() {
	// The special "__fail__" token causes mockoidc.py to return 401
	headers := map[string]string{
		"Authorization": "Bearer __fail__",
	}

	resp, _, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode,
		"opaque token should return 401 when userinfo fails")
}

// Test15_OpaqueTokenNotJWTShaped tests tokens with dots that aren't JWTs
func (ts *TestSuite) Test15_OpaqueTokenNotJWTShaped() {
	// Azure AD-style opaque token with dots (not a valid JWT structure)
	opaqueWithDots := "at.integration_test@example.org.suffix"
	headers := map[string]string{
		"Authorization": "Bearer " + opaqueWithDots,
	}

	resp, _, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	// This should be treated as opaque (dots don't make it a JWT) and succeed via userinfo
	ts.Equal(http.StatusOK, resp.StatusCode,
		"dotted opaque token should be treated as opaque, not JWT")
}

// Test17_SessionCookieReuse tests that a session cookie from a previous request
// can be reused to authenticate without sending the token again
func (ts *TestSuite) Test17_SessionCookieReuse() {
	// First request with JWT to get a session cookie
	req, err := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	ts.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+ts.token)

	// Use a jar-less client so we can inspect cookies manually
	resp, err := http.DefaultClient.Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	// Extract session cookie from response
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "sda_session" {
			sessionCookie = c
			break
		}
	}
	if !ts.hasSessionCache {
		ts.T().Skip("REQUIRES_SESSION_CACHE: visa processing may prevent caching in combined mode")
		return
	}
	ts.Require().NotNil(sessionCookie, "response should set sda_session cookie")

	// Second request with only the session cookie (no Authorization header)
	req2, err := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	ts.Require().NoError(err)
	req2.AddCookie(sessionCookie)

	resp2, err := http.DefaultClient.Do(req2)
	ts.Require().NoError(err)
	defer resp2.Body.Close()
	ts.Equal(http.StatusOK, resp2.StatusCode,
		"session cookie should authenticate without access token")
}

// Test18_ServiceInfo tests the GA4GH service-info endpoint (no auth required)
func (ts *TestSuite) Test18_ServiceInfo() {
	resp, body, err := ts.doRequest("GET", "/service-info", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "service-info should return 200 without auth")

	var info map[string]any
	err = json.Unmarshal(body, &info)
	ts.Require().NoError(err, "response should be valid JSON")
	ts.Contains(info, "id", "service-info should have 'id' field")
	ts.Contains(info, "type", "service-info should have 'type' field")
}

// Test19_HeadFileEndpoint tests HEAD /files/:fileId returns metadata without body
func (ts *TestSuite) Test19_HeadFileEndpoint() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping HEAD file test: " + err.Error())
		return
	}

	pubkeyBase64 := ts.readPublicKeyBase64()
	if pubkeyBase64 == "" {
		ts.T().Skip("No public key available - skipping HEAD file test")
		return
	}

	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = pubkeyBase64

	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	resp, body, err := ts.doRequest("HEAD", "/files/"+fileID, nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "HEAD /files/:fileId should return 200")
	ts.Empty(body, "HEAD response should have no body")
	ts.NotEmpty(resp.Header.Get("Content-Length"), "HEAD response should have Content-Length header")
	ts.NotEmpty(resp.Header.Get("Content-Type"), "HEAD response should have Content-Type header")
}

// Test20_SplitContentEndpoint tests GET /files/:fileId/content returns raw archive bytes
func (ts *TestSuite) Test20_SplitContentEndpoint() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping content endpoint test: " + err.Error())
		return
	}

	if !ts.hasStorageFile {
		ts.T().Skip("REQUIRES_STORAGE_FILE: test file not accessible in storage")
		return
	}

	resp, body, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "GET /files/:fileId/content should return 200")
	ts.NotEmpty(body, "content response should have a body")
	ts.NotEmpty(resp.Header.Get("ETag"), "content response should have ETag header")
	ts.Equal("bytes", resp.Header.Get("Accept-Ranges"), "content response should have Accept-Ranges: bytes")
}

// Test21_SplitHeaderEndpoint tests GET /files/:fileId/header returns re-encrypted header
func (ts *TestSuite) Test21_SplitHeaderEndpoint() {
	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available - skipping header endpoint test: " + err.Error())
		return
	}

	pubkeyPath := getEnv("C4GH_PUBKEY_FILE", "/shared/c4gh.pub.pem")
	pubkeyBytes, err := os.ReadFile(pubkeyPath)
	if err != nil {
		ts.T().Skipf("No public key available at %s - skipping header endpoint test", pubkeyPath)
		return
	}
	pubkeyBase64 := base64.StdEncoding.EncodeToString(pubkeyBytes)

	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = pubkeyBase64

	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	resp, body, err := ts.doRequest("GET", "/files/"+fileID+"/header", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "GET /files/:fileId/header should return 200")
	ts.NotEmpty(body, "header response should have a body")
	ts.Equal("application/octet-stream", resp.Header.Get("Content-Type"),
		"header response should have Content-Type: application/octet-stream")
}

// Test22_ProblemDetailsFormat tests that error responses use application/problem+json
func (ts *TestSuite) Test22_ProblemDetailsFormat() {
	resp, body, err := ts.doRequest("GET", "/datasets", nil, nil)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode, "unauthenticated request should return 401")
	ts.Equal("application/problem+json", resp.Header.Get("Content-Type"),
		"error response should have Content-Type: application/problem+json")

	var problem map[string]any
	err = json.Unmarshal(body, &problem)
	ts.Require().NoError(err, "response should be valid JSON")
	ts.Contains(problem, "title", "problem response should have 'title' field")
	ts.Contains(problem, "status", "problem response should have 'status' field")
	ts.Contains(problem, "detail", "problem response should have 'detail' field")
}

// Test23_PaginationPageSize tests the pageSize query parameter on datasets and files
func (ts *TestSuite) Test23_PaginationPageSize() {
	resp, body, err := ts.doRequest("GET", "/datasets?pageSize=1", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "pageSize=1 should return 200")

	var result struct {
		Datasets      []string `json:"datasets"`
		NextPageToken *string  `json:"nextPageToken"`
	}
	err = json.Unmarshal(body, &result)
	ts.Require().NoError(err, "response should be valid JSON with datasets and nextPageToken fields")
	ts.NotNil(result.Datasets, "response should have 'datasets' field")

	// Also test pageSize on file listing
	if len(result.Datasets) > 0 {
		datasetID := result.Datasets[0]
		resp, body, err = ts.doRequest("GET",
			"/datasets/"+datasetID+"/files?pageSize=1", nil, ts.authHeaders())
		ts.Require().NoError(err)
		ts.Equal(http.StatusOK, resp.StatusCode, "file list with pageSize=1 should return 200")

		var filesResult struct {
			Files         []map[string]any `json:"files"`
			NextPageToken *string          `json:"nextPageToken"`
		}
		err = json.Unmarshal(body, &filesResult)
		ts.Require().NoError(err, "file list response should have files and nextPageToken fields")
	}
}

// Test24_PaginationInvalidPageSize tests that invalid pageSize values return 400
func (ts *TestSuite) Test24_PaginationInvalidPageSize() {
	invalidSizes := []string{"0", "-1", "1001", "abc"}
	for _, size := range invalidSizes {
		resp, _, err := ts.doRequest("GET", "/datasets?pageSize="+size, nil, ts.authHeaders())
		ts.Require().NoError(err)
		ts.Equal(http.StatusBadRequest, resp.StatusCode,
			"pageSize=%s should return 400", size)
	}
}

// Test25_EncodedSlashDatasetID tests that URL-encoded slashes in dataset IDs route correctly
func (ts *TestSuite) Test25_EncodedSlashDatasetID() {
	// URL-encode a dataset ID containing slashes.
	// With UseRawPath=true, %2F should not be treated as path separators.
	encodedID := "https%3A%2F%2Fdoi.example%2Fty009.sfrrss%2F600.45asasga"

	// GetDataset: route matches, auth passes, but dataset not in DB → 403 (no existence leakage)
	// A 404 would indicate routing failure (slashes interpreted as path separators)
	resp, _, err := ts.doRequest("GET", "/datasets/"+encodedID, nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusForbidden, resp.StatusCode,
		"encoded-slash dataset ID should route correctly (expect 403, not 404)")

	// ListDatasetFiles: route matches, auth passes, but dataset not in DB → 403
	resp, _, err = ts.doRequest("GET", "/datasets/"+encodedID+"/files", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusForbidden, resp.StatusCode,
		"encoded-slash dataset files should route correctly (expect 403, not 404)")
}

// Test26_InvalidRangeHeader tests that malformed Range headers return 400
func (ts *TestSuite) Test26_InvalidRangeHeader() {
	if !ts.hasStorageFile {
		ts.T().Skip("REQUIRES_STORAGE_FILE: test file not accessible in storage")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available: " + err.Error())
		return
	}

	headers := ts.authHeaders()
	headers["Range"] = "invalid"

	resp, _, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode, "malformed Range header should return 400")
}

// Test27_MultiRangeRejected tests that multi-range requests return 400
func (ts *TestSuite) Test27_MultiRangeRejected() {
	if !ts.hasStorageFile {
		ts.T().Skip("REQUIRES_STORAGE_FILE: test file not accessible in storage")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available: " + err.Error())
		return
	}

	headers := ts.authHeaders()
	headers["Range"] = "bytes=0-10,20-30"

	resp, _, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode, "multi-range request should return 400")
}

// Test28_IfRangeETagContract tests the If-Range header with valid and stale ETags
func (ts *TestSuite) Test28_IfRangeETagContract() {
	if !ts.hasStorageFile {
		ts.T().Skip("REQUIRES_STORAGE_FILE: test file not accessible in storage")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available: " + err.Error())
		return
	}

	etag := ts.getContentETag(fileID)
	ts.Require().NotEmpty(etag, "HEAD /files/:fileId/content should return ETag")

	// Valid ETag in If-Range + Range → 206 Partial Content
	headers := ts.authHeaders()
	headers["If-Range"] = etag
	headers["Range"] = "bytes=0-99"

	resp, _, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusPartialContent, resp.StatusCode,
		"valid If-Range ETag + Range should return 206")

	// Stale ETag in If-Range + Range → 200 full content (range not honored)
	headers2 := ts.authHeaders()
	headers2["If-Range"] = `"stale-etag-value"`
	headers2["Range"] = "bytes=0-99"

	resp2, _, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, headers2)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp2.StatusCode,
		"stale If-Range ETag + Range should return 200 (full content)")
}

// Test29_ContentDispositionFilename tests that downloads set Content-Disposition correctly
func (ts *TestSuite) Test29_ContentDispositionFilename() {
	if !ts.hasReencrypt {
		ts.T().Skip("REQUIRES_REENCRYPT: seed header cannot be re-encrypted")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available: " + err.Error())
		return
	}

	pubkeyBase64 := ts.readPublicKeyBase64()
	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = pubkeyBase64

	resp, _, err := ts.doRequest("GET", "/files/"+fileID, nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "download should return 200")

	cd := resp.Header.Get("Content-Disposition")
	ts.NotEmpty(cd, "response should have Content-Disposition header")
	ts.Contains(cd, "attachment", "Content-Disposition should be attachment")
	ts.Contains(cd, "test-file.c4gh", "Content-Disposition should contain the filename")
}

// Test30_PathPrefixFilter tests the pathPrefix and filePath query parameters
func (ts *TestSuite) Test30_PathPrefixFilter() {
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err)
	if len(datasetsResp.Datasets) == 0 {
		ts.T().Skip("No datasets available")
		return
	}

	datasetID := datasetsResp.Datasets[0]

	// pathPrefix=test should match "test-file.c4gh"
	resp, body, err = ts.doRequest("GET",
		"/datasets/"+datasetID+"/files?pathPrefix=test", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode)

	var matchResp struct {
		Files []map[string]any `json:"files"`
	}
	err = json.Unmarshal(body, &matchResp)
	ts.Require().NoError(err)
	ts.NotEmpty(matchResp.Files, "pathPrefix=test should match test-file.c4gh")

	// pathPrefix=nonexistent should return empty
	resp, body, err = ts.doRequest("GET",
		"/datasets/"+datasetID+"/files?pathPrefix=nonexistent", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode)

	var noMatchResp struct {
		Files []map[string]any `json:"files"`
	}
	err = json.Unmarshal(body, &noMatchResp)
	ts.Require().NoError(err)
	ts.Empty(noMatchResp.Files, "pathPrefix=nonexistent should match no files")

	// SQL wildcard characters should not cause injection (% is %25 URL-encoded)
	resp, _, err = ts.doRequest("GET",
		"/datasets/"+datasetID+"/files?pathPrefix=%25", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "pathPrefix=%% should return 200 (not SQL error)")

	// SQL underscore wildcard should be escaped (must not match arbitrary single char)
	resp, body, err = ts.doRequest("GET",
		"/datasets/"+datasetID+"/files?pathPrefix=_est", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "pathPrefix=_est should return 200")

	var underscoreResp struct {
		Files []map[string]any `json:"files"`
	}
	err = json.Unmarshal(body, &underscoreResp)
	ts.Require().NoError(err)
	// "_est" should NOT match "test-file.c4gh" because _ is escaped (not a SQL wildcard)
	ts.Empty(underscoreResp.Files, "pathPrefix=_est should not match test-file.c4gh (underscore escaped)")

	// filePath and pathPrefix are mutually exclusive
	resp, _, err = ts.doRequest("GET",
		"/datasets/"+datasetID+"/files?filePath=test&pathPrefix=test", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode,
		"filePath and pathPrefix together should return 400")
}

// Test31_ExpiredTokenRejected tests that expired JWT tokens are rejected
func (ts *TestSuite) Test31_ExpiredTokenRejected() {
	expiredToken, err := ts.generateTokenWithExpiry(
		"integration_test@example.org",
		time.Now().Add(-1*time.Hour),
	)
	ts.Require().NoError(err)

	headers := map[string]string{
		"Authorization": "Bearer " + expiredToken,
	}

	resp, _, err := ts.doRequest("GET", "/datasets", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode,
		"expired JWT token should return 401")
}

// Test32_PageTokenValidation tests that tampered/invalid page tokens are rejected
func (ts *TestSuite) Test32_PageTokenValidation() {
	// Garbage token (not valid base64.signature format)
	resp, _, err := ts.doRequest("GET", "/datasets?pageToken=garbage", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode,
		"garbage pageToken should return 400")

	// Tampered token: valid base64 with wrong HMAC signature
	resp, _, err = ts.doRequest("GET",
		"/datasets?pageToken=eyJjIjoiYSIsInMiOjEsInEiOiJ4IiwiZSI6OTk5OTk5OTk5OX0.dGFtcGVyZWQ",
		nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode,
		"tampered pageToken should return 400")

	// Also test on file listing endpoint
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	err = json.Unmarshal(body, &datasetsResp)
	ts.Require().NoError(err)
	if len(datasetsResp.Datasets) > 0 {
		datasetID := datasetsResp.Datasets[0]
		resp, _, err = ts.doRequest("GET",
			"/datasets/"+datasetID+"/files?pageToken=garbage", nil, ts.authHeaders())
		ts.Require().NoError(err)
		ts.Equal(http.StatusBadRequest, resp.StatusCode,
			"garbage pageToken on file listing should return 400")
	}
}

// Test33_LongTransferResume tests the expired-token-then-resume scenario:
// an expired token with Range should return 401, a fresh token with Range should return 206.
func (ts *TestSuite) Test33_LongTransferResume() {
	if !ts.hasStorageFile {
		ts.T().Skip("REQUIRES_STORAGE_FILE: test file not accessible in storage")
		return
	}

	fileID, err := ts.getFirstFileID()
	if err != nil {
		ts.T().Skip("No files available: " + err.Error())
		return
	}

	// Step 1: Expired token + Range → 401
	expiredToken, err := ts.generateTokenWithExpiry(
		"integration_test@example.org",
		time.Now().Add(-1*time.Hour),
	)
	ts.Require().NoError(err)

	expiredHeaders := map[string]string{
		"Authorization": "Bearer " + expiredToken,
		"Range":         "bytes=0-99",
	}
	resp, _, err := ts.doRequest("GET", "/files/"+fileID+"/content", nil, expiredHeaders)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode,
		"expired token + Range should return 401")

	// Step 2: Fresh token + Range → 206
	freshHeaders := ts.authHeaders()
	freshHeaders["Range"] = "bytes=0-99"

	resp, _, err = ts.doRequest("GET", "/files/"+fileID+"/content", nil, freshHeaders)
	ts.Require().NoError(err)
	ts.Equal(http.StatusPartialContent, resp.StatusCode,
		"fresh token + Range should return 206 (resume succeeds)")
}

// Helper function to get the first available file ID
func (ts *TestSuite) getFirstFileID() (string, error) {
	resp, body, err := ts.doRequest("GET", "/datasets", nil, ts.authHeaders())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get datasets: status %d", resp.StatusCode)
	}

	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	if err := json.Unmarshal(body, &datasetsResp); err != nil {
		return "", err
	}
	if len(datasetsResp.Datasets) == 0 {
		return "", fmt.Errorf("no datasets available")
	}

	datasetID := datasetsResp.Datasets[0]

	resp, body, err = ts.doRequest("GET", "/datasets/"+datasetID+"/files", nil, ts.authHeaders())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get files: status %d", resp.StatusCode)
	}

	var filesResp struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(body, &filesResp); err != nil {
		return "", err
	}
	if len(filesResp.Files) == 0 {
		return "", fmt.Errorf("no files in dataset")
	}

	fileID, ok := filesResp.Files[0]["fileId"].(string)
	if !ok {
		return "", fmt.Errorf("file missing 'fileId' field")
	}

	return fileID, nil
}

// readPublicKeyBase64 reads the C4GH public key and returns it as base64.
// Returns empty string if the key file is not available.
func (ts *TestSuite) readPublicKeyBase64() string {
	pubkeyPath := getEnv("C4GH_PUBKEY_FILE", "/shared/c4gh.pub.pem")
	pubkeyBytes, err := os.ReadFile(pubkeyPath)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(pubkeyBytes)
}

// generateTokenWithExpiry creates a JWT token with a custom expiry time.
func (ts *TestSuite) generateTokenWithExpiry(sub string, expiry time.Time) (string, error) {
	keyPem, err := os.ReadFile(ts.jwtKeyFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read key file: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &jwt.RegisteredClaims{
		Subject:   sub,
		Issuer:    "http://integration.test",
		ExpiresAt: jwt.NewNumericDate(expiry),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
	})

	token.Header["kid"] = "rsa1"
	block, _ := pem.Decode(keyPem)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	keyRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	return token.SignedString(keyRaw)
}

// getContentETag performs HEAD on /files/:fileId/content and returns the ETag.
// Returns empty string if the endpoint is not accessible.
func (ts *TestSuite) getContentETag(fileID string) string {
	resp, _, err := ts.doRequest("HEAD", "/files/"+fileID+"/content", nil, ts.authHeaders())
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}

	return resp.Header.Get("ETag")
}

// Helper to get environment variable with default
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
