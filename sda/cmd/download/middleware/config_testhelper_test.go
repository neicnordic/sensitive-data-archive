//go:build visas
// +build visas

package middleware

import (
	"os"
	"sync"
	"testing"

	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/stretchr/testify/require"
)

var loadConfigOnce sync.Once

func ensureTestConfig(t *testing.T) {
	t.Helper()

	loadConfigOnce.Do(func() {
		t.Setenv("DB_HOST", "localhost")
		t.Setenv("DB_USER", "test")
		t.Setenv("DB_PASSWORD", "test")
		t.Setenv("GRPC_HOST", "localhost")
		t.Setenv("AUTH_ALLOW_OPAQUE", "true")
		t.Setenv("OIDC_ISSUER", "https://issuer.example")
		t.Setenv("VISA_CACHE_TOKEN_TTL", "7200")

		oldArgs := os.Args
		os.Args = []string{oldArgs[0]}
		defer func() { os.Args = oldArgs }()

		require.NoError(t, configv2.Load())
	})
}
