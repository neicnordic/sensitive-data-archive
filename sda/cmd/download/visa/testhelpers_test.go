//go:build visas

package visa

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/require"
)

type fakeDatasetChecker struct {
	existing map[string]bool
}

func (f *fakeDatasetChecker) CheckDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	return f.existing[datasetID], nil
}

func newRSAKeyPair(t *testing.T) (*rsa.PrivateKey, jwk.Key, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubKey, err := jwk.FromRaw(&priv.PublicKey)
	require.NoError(t, err)

	kid := "test-key"
	require.NoError(t, pubKey.Set(jwk.KeyIDKey, kid))

	return priv, pubKey, kid
}

func newJWKSServer(t *testing.T, key jwk.Key) *httptest.Server {
	t.Helper()

	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))

	payload, err := json.Marshal(set)
	require.NoError(t, err)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func newFailingJWKSServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func newUserinfoServer(t *testing.T, passports []string) *httptest.Server {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"sub":               "user-123",
		"ga4gh_passport_v1": passports,
	})
	require.NoError(t, err)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func signVisaJWT(t *testing.T, priv *rsa.PrivateKey, jku, kid, iss, sub string, visa map[string]any, exp time.Time) string {
	t.Helper()

	token := jwt.New()
	require.NoError(t, token.Set(jwt.IssuerKey, iss))
	require.NoError(t, token.Set(jwt.SubjectKey, sub))
	require.NoError(t, token.Set(jwt.ExpirationKey, exp))
	require.NoError(t, token.Set(jwt.IssuedAtKey, time.Now().Add(-1*time.Minute)))
	require.NoError(t, token.Set(jwt.NotBeforeKey, time.Now().Add(-1*time.Minute)))
	require.NoError(t, token.Set("ga4gh_visa_v1", visa))

	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jwk.KeyIDKey, kid))
	require.NoError(t, headers.Set("jku", jku))

	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, priv, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)

	return string(signed)
}
