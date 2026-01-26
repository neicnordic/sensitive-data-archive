package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGetToken_BearerToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token-123")

	token, code, err := GetToken(headers)

	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "test-token-123", token)
}

func TestGetToken_AmzSecurityToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Amz-Security-Token", "amz-token-456")

	token, code, err := GetToken(headers)

	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "amz-token-456", token)
}

func TestGetToken_AmzSecurityTokenPriority(t *testing.T) {
	// X-Amz-Security-Token should take priority over Authorization
	headers := http.Header{}
	headers.Set("X-Amz-Security-Token", "amz-token")
	headers.Set("Authorization", "Bearer bearer-token")

	token, code, err := GetToken(headers)

	assert.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "amz-token", token)
}

func TestGetToken_MissingToken(t *testing.T) {
	headers := http.Header{}

	token, code, err := GetToken(headers)

	assert.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "access token must be provided")
}

func TestGetToken_InvalidScheme(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Basic dXNlcjpwYXNz")

	token, code, err := GetToken(headers)

	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "bearer")
}

func TestGetToken_InvalidFormat(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "BearerNoSpace")

	token, code, err := GetToken(headers)

	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Empty(t, token)
}

func TestGetToken_CaseInsensitiveBearer(t *testing.T) {
	testCases := []string{"Bearer", "bearer", "BEARER", "BeArEr"}

	for _, scheme := range testCases {
		headers := http.Header{}
		headers.Set("Authorization", scheme+" test-token")

		token, code, err := GetToken(headers)

		assert.NoError(t, err, "scheme: %s", scheme)
		assert.Equal(t, 0, code, "scheme: %s", scheme)
		assert.Equal(t, "test-token", token, "scheme: %s", scheme)
	}
}

func TestGetAuthContext_Exists(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	expected := AuthContext{
		Subject:  "user@example.com",
		Datasets: []string{"dataset1", "dataset2"},
	}
	c.Set(ContextKey, expected)

	authCtx, exists := GetAuthContext(c)

	assert.True(t, exists)
	assert.Equal(t, expected.Subject, authCtx.Subject)
	assert.Equal(t, expected.Datasets, authCtx.Datasets)
}

func TestGetAuthContext_NotExists(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	authCtx, exists := GetAuthContext(c)

	assert.False(t, exists)
	assert.Empty(t, authCtx.Subject)
	assert.Empty(t, authCtx.Datasets)
}

func TestSessionCache_SetAndGet(t *testing.T) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	assert.NoError(t, err)

	sc := &SessionCache{cache: cache}

	authCtx := AuthContext{
		Subject:  "user@example.com",
		Datasets: []string{"test-dataset"},
	}

	sc.Set("test-key", authCtx, time.Hour)

	// Wait for cache to process
	time.Sleep(10 * time.Millisecond)

	retrieved, exists := sc.Get("test-key")

	assert.True(t, exists)
	assert.Equal(t, authCtx.Subject, retrieved.Subject)
	assert.Equal(t, authCtx.Datasets, retrieved.Datasets)
}

func TestSessionCache_GetNotExists(t *testing.T) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	assert.NoError(t, err)

	sc := &SessionCache{cache: cache}

	_, exists := sc.Get("nonexistent-key")

	assert.False(t, exists)
}

func TestTokenMiddleware_NoToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)

	// Initialize auth middleware for testing
	err := InitAuthForTesting()
	assert.NoError(t, err)

	handler := TokenMiddleware(nil) // nil database - not testing dataset lookup
	handler(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTokenMiddleware_WithSessionCookie(t *testing.T) {
	// This test requires config to be initialized with session name
	// In a real scenario, config.SessionName() would return the cookie name
	// For unit testing, we skip this as it requires full config initialization
	t.Skip("Requires full config initialization - covered by integration tests")
}

// Integration test helper - creates a test server with middleware
func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Initialize session cache
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     100000,
		BufferItems: 64,
	})
	sessionCache = &SessionCache{cache: cache}

	return r
}

func TestAuthenticator_NoKeys(t *testing.T) {
	auth := &Authenticator{
		Keyset: jwk.NewSet(), // Empty keyset
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	_, err := auth.Authenticate(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no keys configured")
}
