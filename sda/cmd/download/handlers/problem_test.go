package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProblemJSON_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	problemJSON(c, http.StatusBadRequest, "something went wrong")

	assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
}

func TestProblemJSON_ResponseBody(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("correlationId", "test-correlation-id")

	problemJSON(c, http.StatusNotFound, "resource not found")

	assert.Equal(t, http.StatusNotFound, w.Code)

	var pd ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &pd)
	require.NoError(t, err)
	assert.Equal(t, "Not Found", pd.Title)
	assert.Equal(t, http.StatusNotFound, pd.Status)
	assert.Equal(t, "resource not found", pd.Detail)
	assert.Equal(t, "test-correlation-id", pd.CorrelationID)
	assert.Empty(t, pd.ErrorCode)
}

func TestProblemJSONWithCode_IncludesErrorCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("correlationId", "abc-123")

	problemJSONWithCode(c, http.StatusBadRequest, "invalid range", "RANGE_INVALID")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))

	var pd ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &pd)
	require.NoError(t, err)
	assert.Equal(t, "Bad Request", pd.Title)
	assert.Equal(t, http.StatusBadRequest, pd.Status)
	assert.Equal(t, "invalid range", pd.Detail)
	assert.Equal(t, "RANGE_INVALID", pd.ErrorCode)
	assert.Equal(t, "abc-123", pd.CorrelationID)
}

func TestCorrelationIDMiddleware_GeneratesUUID(t *testing.T) {
	w := httptest.NewRecorder()
	c, router := gin.CreateTestContext(w)

	router.Use(correlationIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetString("correlationId")})
	})

	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, c.Request)

	// Check the response header is set
	headerID := w.Header().Get("X-Correlation-ID")
	assert.NotEmpty(t, headerID)
	assert.Len(t, headerID, 36) // UUID format: 8-4-4-4-12

	// Check the context value matches the header
	var body map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, headerID, body["id"])
}

func TestCorrelationIDMiddleware_PropagatesToProblemResponse(t *testing.T) {
	w := httptest.NewRecorder()
	c, router := gin.CreateTestContext(w)

	router.Use(correlationIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		problemJSON(c, http.StatusInternalServerError, "something broke")
	})

	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, c.Request)

	headerID := w.Header().Get("X-Correlation-ID")
	assert.NotEmpty(t, headerID)

	var pd ProblemDetails
	err := json.Unmarshal(w.Body.Bytes(), &pd)
	require.NoError(t, err)
	assert.Equal(t, headerID, pd.CorrelationID)
	assert.Equal(t, http.StatusInternalServerError, pd.Status)
	assert.Equal(t, "Internal Server Error", pd.Title)
	assert.Equal(t, "something broke", pd.Detail)
}
