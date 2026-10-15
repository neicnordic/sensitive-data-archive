package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rotatingJWKS serves a mutable JWKS and counts fetches.
type rotatingJWKS struct {
	*httptest.Server
	mu   sync.Mutex
	keys jwk.Set
	fail bool
	hits atomic.Int32
}

func newRotatingJWKSServer(t *testing.T, keys ...jwk.Key) *rotatingJWKS {
	t.Helper()

	s := &rotatingJWKS{keys: jwk.NewSet()}
	for _, k := range keys {
		s.addKey(t, k)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.keys)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *rotatingJWKS) addKey(t *testing.T, k jwk.Key) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NoError(t, s.keys.AddKey(k))
}

func (s *rotatingJWKS) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

func newKidKeyPair(t *testing.T, kid string) (*rsa.PrivateKey, jwk.Key) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := jwk.FromRaw(&priv.PublicKey)
	require.NoError(t, err)
	require.NoError(t, pub.Set(jwk.KeyIDKey, kid))

	return priv, pub
}

func signWithKid(t *testing.T, priv *rsa.PrivateKey, kid string, exp time.Time) string {
	t.Helper()

	token := jwt.New()
	require.NoError(t, token.Set(jwt.IssuerKey, "https://issuer.example"))
	require.NoError(t, token.Set(jwt.SubjectKey, "user"))
	require.NoError(t, token.Set(jwt.ExpirationKey, exp))

	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jwk.KeyIDKey, kid))
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, priv, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)

	return string(signed)
}

// newURLAuthenticator mirrors InitAuth for a path keyset plus a JWKS URL.
func newURLAuthenticator(t *testing.T, url string, static jwk.Set) *Authenticator {
	t.Helper()

	a := &Authenticator{Keyset: static, staticKeys: static, jwksURL: url}
	require.NoError(t, a.loadKeysFromURL(url))
	a.lastRefresh = time.Now()

	return a
}

// expireRefreshInterval lets the next unknown kid refetch, as if
// jwksMinRefreshInterval had passed.
func expireRefreshInterval(a *Authenticator) {
	a.refreshMu.Lock()
	a.lastRefresh = time.Time{}
	a.refreshMu.Unlock()
}

func TestVerifyJWT_RefetchesJWKSOnUnknownKid(t *testing.T) {
	ensureTestConfig(t)
	exp := time.Now().Add(time.Hour)

	staticPriv, staticPub := newKidKeyPair(t, "static")
	static := jwk.NewSet()
	require.NoError(t, static.AddKey(staticPub))

	privA, pubA := newKidKeyPair(t, "a")
	srv := newRotatingJWKSServer(t, pubA)
	a := newURLAuthenticator(t, srv.URL, static)
	require.Equal(t, int32(1), srv.hits.Load())

	_, err := a.verifyJWT(signWithKid(t, privA, "a", exp))
	require.NoError(t, err)
	assert.Equal(t, int32(1), srv.hits.Load(), "known kid must not refetch")

	// Issuer rotates: new key published after startup.
	privB, pubB := newKidKeyPair(t, "b")
	srv.addKey(t, pubB)
	expireRefreshInterval(a)

	_, err = a.verifyJWT(signWithKid(t, privB, "b", exp))
	require.NoError(t, err)
	assert.Equal(t, int32(2), srv.hits.Load())

	// Static path keys survive the refetch.
	_, err = a.verifyJWT(signWithKid(t, staticPriv, "static", exp))
	assert.NoError(t, err)
}

func TestVerifyJWT_RefetchIsRateLimited(t *testing.T) {
	ensureTestConfig(t)
	exp := time.Now().Add(time.Hour)

	_, pubA := newKidKeyPair(t, "a")
	srv := newRotatingJWKSServer(t, pubA)
	a := newURLAuthenticator(t, srv.URL, jwk.NewSet())

	privX, _ := newKidKeyPair(t, "x")
	unknown := signWithKid(t, privX, "x", exp)

	_, err := a.verifyJWT(unknown)
	require.Error(t, err)
	assert.Equal(t, int32(1), srv.hits.Load(), "the startup fetch counts as a refresh")

	expireRefreshInterval(a)
	_, err = a.verifyJWT(unknown)
	require.Error(t, err)
	assert.Equal(t, int32(2), srv.hits.Load())

	_, err = a.verifyJWT(unknown)
	require.Error(t, err)
	assert.Equal(t, int32(2), srv.hits.Load(), "second refetch within interval must be skipped")

	// Concurrent unknown-kid requests after the interval share one refetch.
	privB, pubB := newKidKeyPair(t, "b")
	srv.addKey(t, pubB)
	expireRefreshInterval(a)

	tokenB := signWithKid(t, privB, "b", exp)
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = a.verifyJWT(tokenB)
		})
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(3), srv.hits.Load())
}

func TestLoadKeysFromURL_StalledBodyTimesOut(t *testing.T) {
	orig := jwksFetchTimeout
	jwksFetchTimeout = 200 * time.Millisecond
	t.Cleanup(func() { jwksFetchTimeout = orig })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release // headers sent, body never arrives
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	a := &Authenticator{Keyset: jwk.NewSet()}
	done := make(chan error, 1)
	go func() { done <- a.loadKeysFromURL(srv.URL) }()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("JWKS fetch did not time out on a stalled response body")
	}
}

func TestVerifyJWT_RefetchFailureKeepsKeys(t *testing.T) {
	ensureTestConfig(t)
	exp := time.Now().Add(time.Hour)

	privA, pubA := newKidKeyPair(t, "a")
	srv := newRotatingJWKSServer(t, pubA)
	a := newURLAuthenticator(t, srv.URL, jwk.NewSet())

	srv.setFail(true)
	expireRefreshInterval(a)
	privX, _ := newKidKeyPair(t, "x")
	_, err := a.verifyJWT(signWithKid(t, privX, "x", exp))
	require.Error(t, err)
	assert.Equal(t, int32(2), srv.hits.Load())

	_, err = a.verifyJWT(signWithKid(t, privA, "a", exp))
	assert.NoError(t, err)
}

func TestVerifyJWT_NoRefetchOnOtherErrors(t *testing.T) {
	ensureTestConfig(t)

	privA, pubA := newKidKeyPair(t, "a")
	srv := newRotatingJWKSServer(t, pubA)
	a := newURLAuthenticator(t, srv.URL, jwk.NewSet())

	// Expired token with known kid.
	_, err := a.verifyJWT(signWithKid(t, privA, "a", time.Now().Add(-time.Hour)))
	require.Error(t, err)

	// Bad signature with known kid.
	privOther, _ := newKidKeyPair(t, "other")
	_, err = a.verifyJWT(signWithKid(t, privOther, "a", time.Now().Add(time.Hour)))
	require.Error(t, err)

	assert.Equal(t, int32(1), srv.hits.Load())
}
