package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestExtractPublicKey_PrimaryOnly(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("X-C4GH-Public-Key", "dGVzdC1rZXk=")

	key, errCode, detail := extractPublicKey(c)
	assert.Equal(t, "dGVzdC1rZXk=", key)
	assert.Empty(t, errCode)
	assert.Empty(t, detail)
}

func TestExtractPublicKey_SecondaryOnly(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Htsget-Context-Public-Key", "c2Vjb25kYXJ5LWtleQ==")

	key, errCode, detail := extractPublicKey(c)
	assert.Equal(t, "c2Vjb25kYXJ5LWtleQ==", key)
	assert.Empty(t, errCode)
	assert.Empty(t, detail)
}

func TestExtractPublicKey_BothPresent(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("X-C4GH-Public-Key", "key1")
	c.Request.Header.Set("Htsget-Context-Public-Key", "key2")

	key, errCode, detail := extractPublicKey(c)
	assert.Empty(t, key)
	assert.Equal(t, "KEY_CONFLICT", errCode)
	assert.Contains(t, detail, "only one of")
}

func TestExtractPublicKey_NeitherPresent(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)

	key, errCode, detail := extractPublicKey(c)
	assert.Empty(t, key)
	assert.Equal(t, "KEY_MISSING", errCode)
	assert.Contains(t, detail, "header is required")
}
