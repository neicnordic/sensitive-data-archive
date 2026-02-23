//go:build visas
// +build visas

package visa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverUserinfoURL_RejectsCrossHostRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/.well-known/openid-configuration", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	_, err := DiscoverUserinfoURL(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redirect to different host not allowed")
}

func TestDiscoverUserinfoURL_AllowsSameHostRedirect(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/real-discovery", http.StatusFound)
	})
	mux.HandleFunc("/real-discovery", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp, _ := json.Marshal(map[string]string{
			"userinfo_endpoint": "https://example.org/userinfo",
		})
		_, _ = w.Write(resp)
	})

	url, err := DiscoverUserinfoURL(server.URL)
	require.NoError(t, err)
	assert.Equal(t, "https://example.org/userinfo", url)
}
