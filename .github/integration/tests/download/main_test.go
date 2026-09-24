package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/stretchr/testify/suite"
)

// The seeded file: database_seed creates seedFileID from the real Crypt4GH
// file that scripts/make_download_v2_testfile.sh writes to /shared.
const (
	seedDatasetID     = "EGAD00000000001"
	seedFileID        = "EGAF00000000001"
	seedPlaintextFile = "/shared/testfile"
	seedBodyFile      = "/shared/testfile.body"   // header-stripped archive object
	seedSHA256File    = "/shared/testfile.sha256" // hex SHA-256 of the plaintext
)

// Further files in seedDatasetID, all backed by the same archive object
// (see scripts/seed_download_v2_db.sh).
const (
	unicodeFileID        = "EGAF00000000002"
	unicodeFilePath      = `special/it's a "quoted" fïle.c4gh`
	quotedFileID         = "EGAF00000000003"
	quotedFilePath       = `special/with space "and quote".c4gh`
	multiChecksumFileID  = "EGAF00000000004" // SHA256 and MD5 UNENCRYPTED checksums
	renamedFileID        = "EGAF00000000005"
	renamedSubmittedPath = "special/0-submitted-name.c4gh"
	renamedDownloadPath  = "special/zz-download-name.c4gh" // file_dataset.download_path
	tiedPathFileID       = "EGAF00000000006"               // same effective path as multiChecksumFileID
)

// datasetFile is a file entry from GET /datasets/:datasetId/files.
type datasetFile struct {
	FileID    string         `json:"fileId"`
	FilePath  string         `json:"filePath"`
	Checksums []fileChecksum `json:"checksums"`
}

type fileChecksum struct {
	Type     string `json:"type"`
	Checksum string `json:"checksum"`
}

// TestSuite defines the download service integration test suite
type TestSuite struct {
	suite.Suite

	// Configuration
	downloadURL    string
	jwtKeyFilePath string

	// Generated tokens
	token string

	// Recipient key pair for re-encrypted downloads. It is not the archive key,
	// so a stored header passed through without re-encryption cannot decrypt.
	recipientPublicKey  string // base64 PEM, as sent in X-C4GH-Public-Key
	recipientPrivateKey [32]byte

	// Environment capabilities (probed once in SetupSuite)
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

	ts.recipientPublicKey, ts.recipientPrivateKey, err = generateRecipientKey()
	if err != nil {
		ts.FailNow("failed to generate recipient key", err.Error())
	}

	// Wait for download service to be ready
	ts.waitForService()

	// Probe environment capabilities
	ts.probeCapabilities()
}

// probeCapabilities tests environment-specific features once and caches results.
func (ts *TestSuite) probeCapabilities() {
	// Probe session cookie. The service only sets sda_session on a request it
	// authenticates from scratch, so probe with a token it has not seen.
	if probeToken, tokenErr := ts.generateToken("integration_test@example.org"); tokenErr == nil {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
		req.Header.Set("Authorization", "Bearer "+probeToken)
		cookieResp, cookieErr := http.DefaultClient.Do(req)
		if cookieErr == nil {
			ts.hasSessionCache = sessionCookie(cookieResp) != nil
			cookieResp.Body.Close()
		}
	}

	ts.T().Logf("Environment capabilities: sessionCache=%v", ts.hasSessionCache)
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
		// Unique per call: iat/exp have second precision, so without this two
		// tokens minted for the same subject in the same second are identical
		// and hit the service's token cache instead of authenticating afresh.
		ID: fmt.Sprintf("%d", time.Now().UnixNano()),
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

// sessionCookie returns the sda_session cookie from a response, or nil.
func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "sda_session" {
			return c
		}
	}

	return nil
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

	ts.Require().NotEmpty(datasetsResp.Datasets, "seeded dataset should be listed")

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

	ts.Require().NotEmpty(datasetsResp.Datasets, "seeded dataset should be listed")

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

// Test07_DownloadWithReencryption tests file download with re-encryption and
// proves it end to end by decrypting the download with the recipient key
func (ts *TestSuite) Test07_DownloadWithReencryption() {
	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID, nil, ts.reencryptHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "download should return 200")
	ts.Require().GreaterOrEqual(len(body), 8)
	ts.Equal("crypt4gh", string(body[:8]), "file should have crypt4gh magic bytes")

	ts.Equal(ts.seedPlaintextSHA256(), ts.decryptedSHA256(body),
		"decrypted download should match the seeded plaintext")
}

// Test08_RangeRequest tests partial file download with Range header across
// the boundary between the re-encrypted header and the archive body
func (ts *TestSuite) Test08_RangeRequest() {
	headers := ts.reencryptHeaders()
	resp, _, err := ts.doRequest("HEAD", "/files/"+seedFileID, nil, headers)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	archiveBody := ts.seedArchiveBody()
	totalSize := resp.ContentLength
	headerSize := totalSize - int64(len(archiveBody))
	ts.Require().Positive(headerSize, "HEAD Content-Length should cover header and archive body")

	// The re-encrypted header differs per request (fresh ephemeral key), so
	// only the body part of the range can be compared byte for byte.
	start, end := headerSize-10, headerSize+99
	headers["Range"] = fmt.Sprintf("bytes=%d-%d", start, end)

	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID, nil, headers)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusPartialContent, resp.StatusCode, "range request should return 206")
	ts.Equal(fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize), resp.Header.Get("Content-Range"))
	ts.Require().Len(body, int(end-start+1))
	ts.Equal(archiveBody[:100], body[10:], "range should continue from the header into the archive body")
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
	opaqueToken := "integration_test@example.org"
	headers := map[string]string{
		"Authorization":     "Bearer " + opaqueToken,
		"X-C4GH-Public-Key": ts.recipientPublicKey,
	}

	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID, nil, headers)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "opaque token download should return 200")
	ts.Equal(ts.seedPlaintextSHA256(), ts.decryptedSHA256(body),
		"decrypted download should match the seeded plaintext")
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
	if !ts.hasSessionCache {
		ts.T().Skip("REQUIRES_SESSION_CACHE: visa processing may prevent caching in combined mode")
		return
	}

	// The cookie is only set on a request the service authenticates from
	// scratch, so use a token it has not seen (ts.token is already cached).
	token, err := ts.generateToken("integration_test@example.org")
	ts.Require().NoError(err)

	// First request with the fresh JWT: expect a session cookie.
	// Use a jar-less client so we can inspect cookies manually.
	req, err := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	ts.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	cookie := sessionCookie(resp)
	ts.Require().NotNil(cookie, "first request with a fresh token should set sda_session cookie")

	// Second request with the same JWT is served from the token cache: no new cookie
	req2, err := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	ts.Require().NoError(err)
	req2.Header.Set("Authorization", "Bearer "+token)

	resp2, err := http.DefaultClient.Do(req2)
	ts.Require().NoError(err)
	defer resp2.Body.Close()
	ts.Equal(http.StatusOK, resp2.StatusCode)
	ts.Nil(sessionCookie(resp2), "request served from the token cache should not set a new sda_session cookie")

	// Third request with only the session cookie (no Authorization header)
	req3, err := http.NewRequestWithContext(context.Background(), "GET", ts.downloadURL+"/datasets", nil)
	ts.Require().NoError(err)
	req3.AddCookie(cookie)

	resp3, err := http.DefaultClient.Do(req3)
	ts.Require().NoError(err)
	defer resp3.Body.Close()
	ts.Equal(http.StatusOK, resp3.StatusCode,
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
	resp, body, err := ts.doRequest("HEAD", "/files/"+seedFileID, nil, ts.reencryptHeaders())
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp.StatusCode, "HEAD /files/:fileId should return 200")
	ts.Empty(body, "HEAD response should have no body")
	ts.Greater(resp.ContentLength, int64(len(ts.seedArchiveBody())),
		"HEAD Content-Length should cover the re-encrypted header and the archive body")
	ts.NotEmpty(resp.Header.Get("Content-Type"), "HEAD response should have Content-Type header")
}

// Test20_SplitContentEndpoint tests GET /files/:fileId/content returns raw archive bytes
func (ts *TestSuite) Test20_SplitContentEndpoint() {
	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "GET /files/:fileId/content should return 200")
	ts.True(bytes.Equal(ts.seedArchiveBody(), body), "content response should be the archived body")
	ts.NotEmpty(resp.Header.Get("ETag"), "content response should have ETag header")
	ts.Equal("bytes", resp.Header.Get("Accept-Ranges"), "content response should have Accept-Ranges: bytes")
}

// Test21_SplitHeaderEndpoint tests GET /files/:fileId/header returns a
// re-encrypted header that decrypts the /content body
func (ts *TestSuite) Test21_SplitHeaderEndpoint() {
	resp, header, err := ts.doRequest("GET", "/files/"+seedFileID+"/header", nil, ts.reencryptHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "GET /files/:fileId/header should return 200")
	ts.Require().NotEmpty(header, "header response should have a body")
	ts.Equal("application/octet-stream", resp.Header.Get("Content-Type"),
		"header response should have Content-Type: application/octet-stream")
	contentETag := resp.Header.Get("SDA-Content-ETag")

	resp, content, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "GET /files/:fileId/content should return 200")
	ts.Equal(resp.Header.Get("ETag"), contentETag, "SDA-Content-ETag from /header should match the /content ETag")

	ts.Equal(ts.seedPlaintextSHA256(), ts.decryptedSHA256(append(header, content...)),
		"/header followed by /content should decrypt to the seeded plaintext")
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
	headers := ts.authHeaders()
	headers["Range"] = "invalid"

	resp, _, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode, "malformed Range header should return 400")
}

// Test27_MultiRangeRejected tests that multi-range requests return 400
func (ts *TestSuite) Test27_MultiRangeRejected() {
	headers := ts.authHeaders()
	headers["Range"] = "bytes=0-10,20-30"

	resp, _, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Equal(http.StatusBadRequest, resp.StatusCode, "multi-range request should return 400")
}

// Test28_IfRangeETagContract tests the If-Range header with valid and stale ETags
func (ts *TestSuite) Test28_IfRangeETagContract() {
	archiveBody := ts.seedArchiveBody()
	etag := ts.getContentETag(seedFileID)
	ts.Require().NotEmpty(etag, "HEAD /files/:fileId/content should return ETag")

	// Valid ETag in If-Range + Range → 206 Partial Content
	headers := ts.authHeaders()
	headers["If-Range"] = etag
	headers["Range"] = "bytes=0-99"

	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, headers)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusPartialContent, resp.StatusCode,
		"valid If-Range ETag + Range should return 206")
	ts.Equal(archiveBody[:100], body, "206 response should be the requested range of the archived body")

	// Stale ETag in If-Range + Range → 200 full content (range not honored)
	headers2 := ts.authHeaders()
	headers2["If-Range"] = `"stale-etag-value"`
	headers2["Range"] = "bytes=0-99"

	resp2, body2, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, headers2)
	ts.Require().NoError(err)
	ts.Equal(http.StatusOK, resp2.StatusCode,
		"stale If-Range ETag + Range should return 200 (full content)")
	ts.True(bytes.Equal(archiveBody, body2), "200 response should be the full archived body")
}

// Test29_ContentDispositionFilename tests that downloads set Content-Disposition correctly
func (ts *TestSuite) Test29_ContentDispositionFilename() {
	resp, _, err := ts.doRequest("GET", "/files/"+seedFileID, nil, ts.reencryptHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "download should return 200")

	disposition, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	ts.Require().NoError(err, "response should have a valid Content-Disposition header")
	ts.Equal("attachment", disposition, "Content-Disposition should be attachment")
	ts.Equal("test-file.c4gh", params["filename"], "Content-Disposition should carry the filename")
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
	ts.Require().NotEmpty(datasetsResp.Datasets, "seeded dataset should be listed")

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
// an expired token with Range should return 401, a fresh token with Range
// should return 206 and the rest of the body from the resume offset.
func (ts *TestSuite) Test33_LongTransferResume() {
	archiveBody := ts.seedArchiveBody()
	resumeFrom := len(archiveBody) / 2
	rangeHeader := fmt.Sprintf("bytes=%d-", resumeFrom)

	// Step 1: Expired token + Range → 401
	expiredToken, err := ts.generateTokenWithExpiry(
		"integration_test@example.org",
		time.Now().Add(-1*time.Hour),
	)
	ts.Require().NoError(err)

	expiredHeaders := map[string]string{
		"Authorization": "Bearer " + expiredToken,
		"Range":         rangeHeader,
	}
	resp, _, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, expiredHeaders)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, resp.StatusCode,
		"expired token + Range should return 401")

	// Step 2: Fresh token + Range → 206
	freshHeaders := ts.authHeaders()
	freshHeaders["Range"] = rangeHeader

	resp, body, err := ts.doRequest("GET", "/files/"+seedFileID+"/content", nil, freshHeaders)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusPartialContent, resp.StatusCode,
		"fresh token + Range should return 206 (resume succeeds)")
	ts.Equal(fmt.Sprintf("bytes %d-%d/%d", resumeFrom, len(archiveBody)-1, len(archiveBody)),
		resp.Header.Get("Content-Range"))
	ts.True(bytes.Equal(archiveBody[resumeFrom:], body), "resumed body should continue from the Range offset")
}

// Test34_PaginationTraversal follows nextPageToken across all pages and checks
// that the pages list every file exactly once, in the unpaginated order
func (ts *TestSuite) Test34_PaginationTraversal() {
	for _, tc := range []struct {
		filter url.Values
		files  []string
	}{
		{url.Values{}, []string{seedFileID, unicodeFileID, quotedFileID, multiChecksumFileID, renamedFileID, tiedPathFileID}},
		{url.Values{"pathPrefix": {"special/"}}, []string{unicodeFileID, quotedFileID, renamedFileID}},
		// Two files share this effective path, so paging has to break the tie
		{url.Values{"pathPrefix": {"multi-checksum"}}, []string{multiChecksumFileID, tiedPathFileID}},
		{url.Values{"filePath": {"multi-checksum.c4gh"}}, []string{multiChecksumFileID, tiedPathFileID}},
	} {
		all := ts.listFilePages(tc.filter)
		ts.Require().Len(all, 1, "default page size should fit the whole listing (%v)", tc.filter)
		want := fileIDs(all[0])
		ts.Require().ElementsMatch(tc.files, want, "unpaginated listing should hold each seeded file once (%v)", tc.filter)

		for _, pageSize := range []int{1, 2} {
			paged := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
			for k, v := range tc.filter {
				paged[k] = v
			}

			pages := ts.listFilePages(paged)
			ts.Len(pages, (len(want)+pageSize-1)/pageSize, "page count for %v", paged)

			var got []string
			for _, page := range pages {
				ts.NotEmpty(page, "no page should be empty (%v)", paged)
				ts.LessOrEqual(len(page), pageSize, "page should respect pageSize (%v)", paged)
				got = append(got, fileIDs(page)...)
			}
			ts.Equal(want, got, "pages should continue without gaps or overlap (%v)", paged)
		}
	}
}

// Test35_MultiChecksumPagination tests that a file with more than one
// UNENCRYPTED checksum is listed once, carrying all its checksums
func (ts *TestSuite) Test35_MultiChecksumPagination() {
	plaintext, err := os.ReadFile(seedPlaintextFile)
	ts.Require().NoError(err)
	wantMD5 := fmt.Sprintf("%x", md5.Sum(plaintext))

	for _, pageSize := range []string{"1", "100"} {
		var listed []datasetFile
		for _, page := range ts.listFilePages(url.Values{"pageSize": {pageSize}}) {
			for _, f := range page {
				if f.FileID == multiChecksumFileID {
					listed = append(listed, f)
				}
			}
		}
		ts.Require().Len(listed, 1, "file with two checksums should be listed once (pageSize=%s)", pageSize)

		ts.ElementsMatch([]fileChecksum{{"sha256", ts.seedPlaintextSHA256()}, {"md5", wantMD5}}, listed[0].Checksums,
			"listing should carry exactly the UNENCRYPTED checksums (pageSize=%s)", pageSize)
	}
}

// Test36_ContentDispositionEscaping tests Content-Disposition for filenames
// with spaces, quotes and non-ASCII characters
func (ts *TestSuite) Test36_ContentDispositionEscaping() {
	for _, tc := range []struct {
		fileID   string
		filename string
	}{
		{unicodeFileID, `it's a "quoted" fïle.c4gh`},
		{quotedFileID, `with space "and quote".c4gh`},
	} {
		resp, _, err := ts.doRequest("GET", "/files/"+tc.fileID, nil, ts.reencryptHeaders())
		ts.Require().NoError(err)
		ts.Require().Equal(http.StatusOK, resp.StatusCode, "download of %s should return 200", tc.fileID)

		cd := resp.Header.Get("Content-Disposition")
		ts.Regexp(`^[\x20-\x7e]+$`, cd, "Content-Disposition should be escaped to printable ASCII")
		disposition, params, err := mime.ParseMediaType(cd)
		ts.Require().NoError(err, "Content-Disposition %q should parse", cd)
		ts.Equal("attachment", disposition)
		ts.Equal(tc.filename, params["filename"], "Content-Disposition %q should round-trip the filename", cd)
	}
}

// Test37_SpecialCharacterPathFilters tests filePath and pathPrefix filtering
// on paths with spaces, quotes and non-ASCII characters
func (ts *TestSuite) Test37_SpecialCharacterPathFilters() {
	for _, tc := range []struct {
		filter url.Values
		fileID string
		path   string
	}{
		{url.Values{"filePath": {unicodeFilePath}}, unicodeFileID, unicodeFilePath},
		{url.Values{"pathPrefix": {`special/it's a "quoted" fï`}}, unicodeFileID, unicodeFilePath},
		{url.Values{"filePath": {quotedFilePath}}, quotedFileID, quotedFilePath},
		{url.Values{"pathPrefix": {`special/with space "and`}}, quotedFileID, quotedFilePath},
	} {
		pages := ts.listFilePages(tc.filter)
		ts.Require().Len(pages, 1)
		ts.Require().Len(pages[0], 1, "%v should match exactly one file", tc.filter)
		ts.Equal(tc.fileID, pages[0][0].FileID, "%v should match %s", tc.filter, tc.fileID)
		ts.Equal(tc.path, pages[0][0].FilePath)
	}
}

// Test38_DownloadPathOverride tests that file_dataset.download_path replaces
// the submitted path in listings, filters and Content-Disposition
func (ts *TestSuite) Test38_DownloadPathOverride() {
	for _, filter := range []url.Values{
		{"filePath": {renamedDownloadPath}},
		{"pathPrefix": {"special/zz-"}},
	} {
		pages := ts.listFilePages(filter)
		ts.Require().Len(pages, 1)
		ts.Require().Len(pages[0], 1, "%v should match the download path", filter)
		ts.Equal(renamedFileID, pages[0][0].FileID)
		ts.Equal(renamedDownloadPath, pages[0][0].FilePath)
	}

	for _, filter := range []url.Values{
		{"filePath": {renamedSubmittedPath}},
		{"pathPrefix": {"special/0-"}},
	} {
		pages := ts.listFilePages(filter)
		ts.Require().Len(pages, 1)
		ts.Empty(pages[0], "%v should not match the overridden submitted path", filter)
	}

	resp, _, err := ts.doRequest("HEAD", "/files/"+renamedFileID, nil, ts.reencryptHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode)
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	ts.Require().NoError(err)
	ts.Equal("zz-download-name.c4gh", params["filename"])
}

// Test39_DrsObjectChecksums tests that the DRS object for the seeded file
// carries the ARCHIVED checksum of the archive body and points at /content
func (ts *TestSuite) Test39_DrsObjectChecksums() {
	resp, body, err := ts.doRequest("GET", "/objects/"+seedDatasetID+"/test-file.c4gh", nil, ts.authHeaders())
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, resp.StatusCode, "DRS object should return 200")

	var obj struct {
		ID            string         `json:"id"`
		Size          int64          `json:"size"`
		Checksums     []fileChecksum `json:"checksums"`
		AccessMethods []struct {
			AccessURL struct {
				URL string `json:"url"`
			} `json:"access_url"`
		} `json:"access_methods"`
	}
	ts.Require().NoError(json.Unmarshal(body, &obj), "DRS object should be valid JSON")

	archiveBody := ts.seedArchiveBody()
	ts.Equal(seedFileID, obj.ID)
	ts.Equal(int64(len(archiveBody)), obj.Size, "DRS size should be the archived body size")
	ts.Equal([]fileChecksum{{"sha-256", fmt.Sprintf("%x", sha256.Sum256(archiveBody))}}, obj.Checksums,
		"DRS checksums should be the ARCHIVED checksum of the body")
	ts.Require().Len(obj.AccessMethods, 1)
	ts.True(strings.HasSuffix(obj.AccessMethods[0].AccessURL.URL, "/files/"+seedFileID+"/content"),
		"DRS access URL %q should point at the content endpoint", obj.AccessMethods[0].AccessURL.URL)
}

// listFilePages lists seedDatasetID's files with the given query parameters,
// following nextPageToken until the last page, and returns every page.
func (ts *TestSuite) listFilePages(query url.Values) [][]datasetFile {
	params := url.Values{}
	for k, v := range query {
		params[k] = v
	}

	var pages [][]datasetFile
	for range 20 {
		path := "/datasets/" + seedDatasetID + "/files?" + params.Encode()
		resp, body, err := ts.doRequest("GET", path, nil, ts.authHeaders())
		ts.Require().NoError(err)
		ts.Require().Equal(http.StatusOK, resp.StatusCode, "GET %s should return 200", path)

		var page struct {
			Files         []datasetFile `json:"files"`
			NextPageToken *string       `json:"nextPageToken"`
		}
		ts.Require().NoError(json.Unmarshal(body, &page), "GET %s should return valid JSON", path)
		pages = append(pages, page.Files)

		if page.NextPageToken == nil {
			return pages
		}
		params.Set("pageToken", *page.NextPageToken)
	}

	ts.FailNow("pagination did not reach a last page", "query %v", query)

	return nil
}

// fileIDs returns the file IDs of a listing page in order.
func fileIDs(files []datasetFile) []string {
	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.FileID)
	}

	return ids
}

// generateRecipientKey creates a Crypt4GH key pair and returns the public key
// as clients send it in X-C4GH-Public-Key: base64 of the PEM encoding.
func generateRecipientKey() (string, [32]byte, error) {
	publicKey, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		return "", privateKey, err
	}

	var pemKey bytes.Buffer
	if err := keys.WriteCrypt4GHX25519PublicKey(&pemKey, publicKey); err != nil {
		return "", privateKey, err
	}

	return base64.StdEncoding.EncodeToString(pemKey.Bytes()), privateKey, nil
}

// reencryptHeaders returns the auth headers plus the recipient public key
// that download endpoints re-encrypt the header for.
func (ts *TestSuite) reencryptHeaders() map[string]string {
	headers := ts.authHeaders()
	headers["X-C4GH-Public-Key"] = ts.recipientPublicKey

	return headers
}

// seedArchiveBody returns the header-stripped body stored as the archive object.
func (ts *TestSuite) seedArchiveBody() []byte {
	body, err := os.ReadFile(seedBodyFile)
	ts.Require().NoError(err, "seeded archive body should be readable")

	return body
}

// seedPlaintextSHA256 returns the hex SHA-256 of the seeded file's plaintext.
func (ts *TestSuite) seedPlaintextSHA256() string {
	sum, err := os.ReadFile(seedSHA256File)
	ts.Require().NoError(err, "seeded plaintext checksum should be readable")

	return strings.TrimSpace(string(sum))
}

// decryptedSHA256 decrypts a Crypt4GH stream with the recipient private key
// and returns the hex SHA-256 of the plaintext. Callers must compare the
// digest: the reader does not report a stream cut at a segment boundary.
func (ts *TestSuite) decryptedSHA256(c4gh []byte) string {
	reader, err := streaming.NewCrypt4GHReader(bytes.NewReader(c4gh), ts.recipientPrivateKey, nil)
	ts.Require().NoError(err, "download should decrypt with the recipient key")
	defer reader.Close()

	hash := sha256.New()
	_, err = io.Copy(hash, reader)
	ts.Require().NoError(err, "download body should decrypt")

	return hex.EncodeToString(hash.Sum(nil))
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
