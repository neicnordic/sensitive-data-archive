//go:build visas

package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeJWT(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	payload := map[string]any{"sub": "user", "exp": time.Now().Add(1 * time.Hour).Unix()}

	token := b64JSON(t, header) + "." + b64JSON(t, payload) + ".signature"
	assert.True(t, looksLikeJWT(token))

	opaque := "abc." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".xyz"
	assert.False(t, looksLikeJWT(opaque))
}

func TestAuthenticate_JWTShapedInvalid_NoUserinfoFallback(t *testing.T) {
	ensureTestConfig(t)

	userinfoCalls := int32(0)
	userinfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&userinfoCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sub":"user-123"}`))
	}))
	t.Cleanup(userinfoServer.Close)

	_, pubA := newKeyPair(t)
	privB, _ := newKeyPair(t)

	keyset := jwk.NewSet()
	require.NoError(t, keyset.AddKey(pubA))

	badToken := signJWT(t, privB, "https://issuer.example", "user-123", time.Now().Add(1*time.Hour))

	auth = &Authenticator{Keyset: keyset}

	userinfoClient, err := visa.NewUserinfoClient(userinfoServer.URL, time.Second)
	require.NoError(t, err)

	_, err = authenticateStructureBased(badToken, userinfoClient)
	assert.Error(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&userinfoCalls))
}

func TestAuthenticate_OpaqueToken_UserinfoFlow(t *testing.T) {
	ensureTestConfig(t)

	userinfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"user-123"}`))
	}))
	t.Cleanup(userinfoServer.Close)

	userinfoClient, err := visa.NewUserinfoClient(userinfoServer.URL, time.Second)
	require.NoError(t, err)

	authCtx, err := authenticateStructureBased("opaque-token", userinfoClient)
	require.NoError(t, err)
	assert.Equal(t, "user-123", authCtx.Subject)
}

func TestAuthenticate_DottyOpaque_UserinfoFlow(t *testing.T) {
	ensureTestConfig(t)

	userinfoCalls := int32(0)
	userinfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&userinfoCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"user-123"}`))
	}))
	t.Cleanup(userinfoServer.Close)

	userinfoClient, err := visa.NewUserinfoClient(userinfoServer.URL, time.Second)
	require.NoError(t, err)

	token := "abc." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".xyz"
	authCtx, err := authenticateStructureBased(token, userinfoClient)
	require.NoError(t, err)
	assert.Equal(t, "user-123", authCtx.Subject)
	assert.Equal(t, int32(1), atomic.LoadInt32(&userinfoCalls))
}

func TestAuthenticate_OpaqueToken_Disallowed(t *testing.T) {
	ensureTestConfig(t)
	setAuthAllowOpaque(t, false)

	_, err := authenticateStructureBased("opaque-token", nil)
	assert.Error(t, err)
}

func b64JSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	return base64.RawURLEncoding.EncodeToString(data)
}

func newKeyPair(t *testing.T) (*rsa.PrivateKey, jwk.Key) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pub, err := jwk.FromRaw(&priv.PublicKey)
	require.NoError(t, err)
	require.NoError(t, jwk.AssignKeyID(pub))

	return priv, pub
}

func signJWT(t *testing.T, priv *rsa.PrivateKey, iss, sub string, exp time.Time) string {
	t.Helper()

	token := jwt.New()
	require.NoError(t, token.Set(jwt.IssuerKey, iss))
	require.NoError(t, token.Set(jwt.SubjectKey, sub))
	require.NoError(t, token.Set(jwt.ExpirationKey, exp))
	require.NoError(t, token.Set(jwt.IssuedAtKey, time.Now().Add(-1*time.Minute)))
	require.NoError(t, token.Set(jwt.NotBeforeKey, time.Now().Add(-1*time.Minute)))

	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jwk.KeyIDKey, "test-key"))

	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, priv, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)

	return string(signed)
}
