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

func TestServiceInfo_Response(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/service-info", nil)

	h, err := New(
		WithDatabase(&mockDatabase{}),
		WithServiceInfo("fi.csc.sda.download", "CSC", "https://csc.fi"),
	)
	require.NoError(t, err)
	h.ServiceInfo(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))

	var response serviceInfoResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "fi.csc.sda.download", response.ID)
	assert.Equal(t, "SDA Download", response.Name)
	assert.Equal(t, "org.ga4gh", response.Type.Group)
	assert.Equal(t, "drs", response.Type.Artifact)
	assert.Equal(t, "1.0.0", response.Type.Version)
	assert.Equal(t, "CSC", response.Organization.Name)
	assert.Equal(t, "https://csc.fi", response.Organization.URL)
	assert.Equal(t, "2.0.0", response.Version)
}
