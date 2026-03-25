//go:build !visas

package middleware

import (
	"os"
	"sync"
	"testing"

	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/stretchr/testify/require"
)

var loadConfigOnceNoVisas sync.Once

func ensureTestConfig(t *testing.T) {
	t.Helper()

	loadConfigOnceNoVisas.Do(func() {
		t.Setenv("DB_HOST", "localhost")
		t.Setenv("DB_USER", "test")
		t.Setenv("DB_PASSWORD", "test")
		t.Setenv("GRPC_HOST", "localhost")
		t.Setenv("AUTH_ALLOW_OPAQUE", "true")

		oldArgs := os.Args
		os.Args = []string{oldArgs[0]}
		defer func() { os.Args = oldArgs }()

		require.NoError(t, configv2.Load())
	})
}
