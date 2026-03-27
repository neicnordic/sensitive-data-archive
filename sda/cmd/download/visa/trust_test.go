//go:build visas

package visa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTrustedIssuers_RejectsHTTPJKUByDefault(t *testing.T) {
	path := writeTrustedIssuersFile(t, []TrustedIssuer{{ISS: "https://issuer.example", JKU: "http://example.org/jwks"}})

	_, err := LoadTrustedIssuers(path, false)
	assert.Error(t, err)
}

func TestLoadTrustedIssuers_AllowsHTTPJKUWhenEnabled(t *testing.T) {
	path := writeTrustedIssuersFile(t, []TrustedIssuer{{ISS: "https://issuer.example", JKU: "http://example.org/jwks"}})

	issuers, err := LoadTrustedIssuers(path, true)
	require.NoError(t, err)
	require.Len(t, issuers, 1)
	assert.Equal(t, "http://example.org/jwks", issuers[0].JKU)
}

func TestNormalizeURL_TrailingSlash(t *testing.T) {
	assert.Equal(t, "https://issuer.example", NormalizeURL("https://issuer.example/"))
	assert.Equal(t, "https://issuer.example", NormalizeURL("https://issuer.example"))
	assert.Equal(t, "https://issuer.example/path", NormalizeURL("https://issuer.example/path/"))
}

func writeTrustedIssuersFile(t *testing.T, issuers []TrustedIssuer) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "trusted-issuers.json")

	payload, err := json.Marshal(issuers)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, payload, 0o600))

	return path
}
