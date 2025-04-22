package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/pkg/auth"
	log "github.com/sirupsen/logrus"
)

const token string = "token"

// testEndpoint mimics the endpoint handlers that perform business logic after passing the
// authentication middleware. This handler is generic and can be used for all cases.
func testEndpoint(_ *gin.Context) {}

func TestTokenMiddleware_Fail_GetToken(t *testing.T) {
	// Save original to-be-mocked functions
	originalGetToken := auth.GetToken

	// Substitute mock functions
	auth.GetToken = func(_ http.Header) (string, int, error) {
		return "", 401, errors.New("access token must be provided")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)

	// Send a request through the middleware
	router.GET("/", TokenMiddleware(), testEndpoint)
	router.ServeHTTP(w, r)

	// Test the outcomes of the handler
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 401
	expectedBody := []byte("access token must be provided")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestTokenMiddleware_Fail_GetToken failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestTokenMiddleware_Fail_GetToken failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	auth.GetToken = originalGetToken
}

func TestTokenMiddleware_Fail_GetVisas(t *testing.T) {
	// Save original to-be-mocked functions
	originalGetToken := auth.GetToken
	originalGetVisas := auth.GetVisas

	// Substitute mock functions
	auth.GetToken = func(_ http.Header) (string, int, error) {
		return token, 200, nil
	}
	auth.GetVisas = func(_ auth.OIDCDetails, _ string) (*auth.Visas, error) {
		return nil, errors.New("get visas failed")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)

	// Send a request through the middleware
	router.GET("/", TokenMiddleware(), testEndpoint)
	router.ServeHTTP(w, r)

	// Test the outcomes of the handler
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 401
	expectedBody := []byte("get visas failed")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestTokenMiddleware_Fail_GetVisas failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestTokenMiddleware_Fail_GetVisas failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	auth.GetToken = originalGetToken
	auth.GetVisas = originalGetVisas
}

func TestTokenMiddleware_Fail_GetPermissions(t *testing.T) {
	// Save original to-be-mocked functions
	originalGetToken := auth.GetToken
	originalGetVisas := auth.GetVisas
	originalGetPermissions := auth.GetPermissions

	// Substitute mock functions
	auth.GetToken = func(_ http.Header) (string, int, error) {
		return token, 200, nil
	}
	auth.GetVisas = func(_ auth.OIDCDetails, _ string) (*auth.Visas, error) {
		return &auth.Visas{}, nil
	}
	auth.GetPermissions = func(_ auth.Visas) []string {
		return []string{}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)

	// Send a request through the middleware
	router.GET("/", TokenMiddleware(), testEndpoint)
	router.ServeHTTP(w, r)

	// Test the outcomes of the handler
	response := w.Result()
	defer response.Body.Close()
	expectedStatusCode := 200

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestTokenMiddleware_Fail_GetPermissions failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}

	// Return mock functions to originals
	auth.GetToken = originalGetToken
	auth.GetVisas = originalGetVisas
	auth.GetPermissions = originalGetPermissions
}

func TestTokenMiddleware_Success_NoCache(t *testing.T) {
	// Save original to-be-mocked functions
	originalGetToken := auth.GetToken
	originalGetVisas := auth.GetVisas
	originalGetPermissions := auth.GetPermissions
	originalNewSessionKey := session.NewSessionKey

	// Substitute mock functions
	auth.GetToken = func(_ http.Header) (string, int, error) {
		return token, 200, nil
	}
	auth.GetVisas = func(_ auth.OIDCDetails, _ string) (*auth.Visas, error) {
		return &auth.Visas{}, nil
	}
	auth.GetPermissions = func(_ auth.Visas) []string {
		return []string{"dataset1", "dataset2"}
	}
	session.NewSessionKey = func() string {
		return "key"
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)

	// Now that we are modifying the request context, we need to place the context test inside the handler
	expectedDatasets := []string{"dataset1", "dataset2"}
	testEndpointWithContextData := func(c *gin.Context) {
		datasets, _ := c.Get(requestContextKey)
		if !reflect.DeepEqual(datasets.(session.Cache).Datasets, expectedDatasets) {
			t.Errorf("TestTokenMiddleware_Success_NoCache failed, got %s expected %s", datasets, expectedDatasets)
		}
	}

	// Send a request through the middleware
	router.GET("/", TokenMiddleware(), testEndpointWithContextData)
	router.ServeHTTP(w, r)

	// Test the outcomes of the handler
	response := w.Result()
	defer response.Body.Close()
	expectedStatusCode := 200
	expectedSessionKey := "key"

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestTokenMiddleware_Success_NoCache failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	// nolint:bodyclose
	for _, c := range w.Result().Cookies() {
		if c.Name == config.Config.Session.Name {
			if c.Value != expectedSessionKey {
				t.Errorf("TestTokenMiddleware_Success_NoCache failed, got %s expected %s", c.Value, expectedSessionKey)
			}
		}
	}

	// Return mock functions to originals
	auth.GetToken = originalGetToken
	auth.GetVisas = originalGetVisas
	auth.GetPermissions = originalGetPermissions
	session.NewSessionKey = originalNewSessionKey
}

func TestTokenMiddleware_Success_FromCache(t *testing.T) {
	// Save original to-be-mocked functions
	originalGetCache := session.Get

	// Substitute mock functions
	session.Get = func(key string) (session.Cache, bool) {
		log.Warningf("session.Get %v", key)
		cached := session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}

		return cached, true
	}

	config.Config.Session.Name = "sda_session_key"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)

	r.AddCookie(&http.Cookie{
		Name:  "sda_session_key",
		Value: "key",
	})

	// Now that we are modifying the request context, we need to place the context test inside the handler
	expectedDatasets := []string{"dataset1", "dataset2"}
	testEndpointWithContextData := func(c *gin.Context) {
		datasets, _ := c.Get(requestContextKey)
		if !reflect.DeepEqual(datasets.(session.Cache).Datasets, expectedDatasets) {
			t.Errorf("TestTokenMiddleware_Success_FromCache failed, got %s expected %s", datasets, expectedDatasets)
		}
	}

	// Send a request through the middleware
	router.GET("/", TokenMiddleware(), testEndpointWithContextData)
	router.ServeHTTP(w, r)

	// Test the outcomes of the handler
	response := w.Result()
	defer response.Body.Close()
	expectedStatusCode := 200

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestTokenMiddleware_Success_FromCache failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	// nolint:bodyclose
	for _, c := range w.Result().Cookies() {
		if c.Name == config.Config.Session.Name {
			t.Errorf("TestTokenMiddleware_Success_FromCache failed, got a session cookie, when should not have")
		}
	}

	// Return mock functions to originals
	session.Get = originalGetCache
}

func TestStoreDatasets(t *testing.T) {
	// Get a request context for testing if data is saved
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Store data to request context
	datasets := session.Cache{
		Datasets: []string{"dataset1", "dataset2"},
	}
	c.Set(requestContextKey, datasets)

	// Verify that context has new data
	storedDatasets := c.Value(requestContextKey).(session.Cache)
	if !reflect.DeepEqual(datasets, storedDatasets) {
		t.Errorf("TestStoreDatasets failed, got %s, expected %s", storedDatasets, datasets)
	}
}

func TestGetDatasets(t *testing.T) {
	// Get a request context for testing if data is saved
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Store data to request context
	datasets := session.Cache{
		Datasets: []string{"dataset1", "dataset2"},
	}
	c.Set(requestContextKey, datasets)

	// Verify that context has new data
	storedDatasets := GetCacheFromContext(c)
	if !reflect.DeepEqual(datasets, storedDatasets) {
		t.Errorf("TestStoreDatasets failed, got %s, expected %s", storedDatasets, datasets)
	}
}
