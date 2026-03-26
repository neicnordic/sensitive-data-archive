//go:build visas

package middleware

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDatasetLookup struct {
	datasets []string
	calls    int32
}

func (f *fakeDatasetLookup) GetDatasetIDsByUser(ctx context.Context, user string) ([]string, error) {
	atomic.AddInt32(&f.calls, 1)

	return f.datasets, nil
}

type fakeDatasetChecker struct {
	existing map[string]bool
}

func (f *fakeDatasetChecker) CheckDatasetExists(ctx context.Context, datasetID string) (bool, error) {
	return f.existing[datasetID], nil
}

type authResponse struct {
	Subject  string   `json:"subject"`
	Datasets []string `json:"datasets"`
	Owned    []string `json:"owned"`
	Visa     []string `json:"visa"`
}

func TestTokenMiddleware_PermissionModels(t *testing.T) {
	ensureTestConfig(t)

	token, _ := setupAuthAndToken(t, "user-123")

	visaValidator := newVisaValidator(t, "visa-1", "https://visa-issuer.example")

	t.Run("ownership", func(t *testing.T) {
		setPermissionModel(t, "ownership")
		db := &fakeDatasetLookup{datasets: []string{"owned-1"}}

		r := newTestRouter(t, db, visaValidator)
		resp := doRequest(t, r, token, "")

		assert.Equal(t, http.StatusOK, resp.Code)
		got := decodeAuthResponse(t, resp.Body)
		assert.ElementsMatch(t, []string{"owned-1"}, got.Datasets)
		assert.Empty(t, got.Visa)
		assert.ElementsMatch(t, []string{"owned-1"}, got.Owned)
	})

	t.Run("visa", func(t *testing.T) {
		setPermissionModel(t, "visa")
		db := &fakeDatasetLookup{datasets: []string{"owned-1"}}

		r := newTestRouter(t, db, visaValidator)
		resp := doRequest(t, r, token, "")

		assert.Equal(t, http.StatusOK, resp.Code)
		got := decodeAuthResponse(t, resp.Body)
		assert.ElementsMatch(t, []string{"visa-1"}, got.Datasets)
		assert.ElementsMatch(t, []string{"visa-1"}, got.Visa)
		assert.Empty(t, got.Owned)
	})

	t.Run("combined", func(t *testing.T) {
		setPermissionModel(t, "combined")
		db := &fakeDatasetLookup{datasets: []string{"owned-1"}}

		r := newTestRouter(t, db, visaValidator)
		resp := doRequest(t, r, token, "")

		assert.Equal(t, http.StatusOK, resp.Code)
		got := decodeAuthResponse(t, resp.Body)
		assert.ElementsMatch(t, []string{"owned-1", "visa-1"}, got.Datasets)
		assert.ElementsMatch(t, []string{"visa-1"}, got.Visa)
		assert.ElementsMatch(t, []string{"owned-1"}, got.Owned)
	})
}

func TestTokenMiddleware_TokenCacheHit(t *testing.T) {
	ensureTestConfig(t)
	setPermissionModel(t, "combined")

	token, _ := setupAuthAndToken(t, "user-123")
	db := &fakeDatasetLookup{datasets: []string{"owned-1"}}

	validator, userinfoCalls := newVisaValidatorWithUserinfoCounter(t, "visa-1", "https://visa-issuer.example")

	r := newTestRouter(t, db, validator)

	resp1 := doRequest(t, r, token, "")
	assert.Equal(t, http.StatusOK, resp1.Code)
	time.Sleep(10 * time.Millisecond)
	resp2 := doRequest(t, r, token, "")
	assert.Equal(t, http.StatusOK, resp2.Code)

	assert.Equal(t, int32(1), atomic.LoadInt32(&db.calls))
	assert.Equal(t, int32(1), atomic.LoadInt32(userinfoCalls))
}

func TestTokenMiddleware_VisaFailureCachingPolicyCombined(t *testing.T) {
	ensureTestConfig(t)
	setPermissionModel(t, "combined")

	token, _ := setupAuthAndToken(t, "user-123")
	db := &fakeDatasetLookup{datasets: []string{"owned-1"}}

	validator, userinfoCalls := newVisaValidatorWithUserinfoFailure(t)

	r := newTestRouter(t, db, validator)

	resp1 := doRequest(t, r, token, "")
	assert.Equal(t, http.StatusOK, resp1.Code)
	resp2 := doRequest(t, r, token, "")
	assert.Equal(t, http.StatusOK, resp2.Code)

	assert.Equal(t, int32(2), atomic.LoadInt32(&db.calls))
	assert.Equal(t, int32(2), atomic.LoadInt32(userinfoCalls))
}

func TestTokenMiddleware_VisaFailureReturns401InVisaMode(t *testing.T) {
	ensureTestConfig(t)
	setPermissionModel(t, "visa")

	token, _ := setupAuthAndToken(t, "user-123")
	validator, _ := newVisaValidatorWithUserinfoFailure(t)

	r := newTestRouter(t, nil, validator)

	resp := doRequest(t, r, token, "")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestTokenMiddleware_LegacyCookieFallback(t *testing.T) {
	ensureTestConfig(t)
	setPermissionModel(t, "combined")

	initCaches(t)
	cached := AuthContext{Subject: "cached-user", Datasets: []string{"ds-1"}}
	sessionCache.Set("legacy", cached, time.Hour)
	time.Sleep(10 * time.Millisecond)

	r := gin.New()
	r.Use(TokenMiddleware(nil, nil, audit.NoopLogger{}))
	r.GET("/test", func(c *gin.Context) {
		ctx, _ := GetAuthContext(c)
		c.JSON(http.StatusOK, gin.H{"subject": ctx.Subject, "datasets": ctx.Datasets})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Cookie", "sda_session_key=legacy")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	var body struct {
		Subject  string   `json:"subject"`
		Datasets []string `json:"datasets"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "cached-user", body.Subject)
	assert.ElementsMatch(t, []string{"ds-1"}, body.Datasets)
}

func TestTokenMiddleware_UserinfoFailureReturns401(t *testing.T) {
	ensureTestConfig(t)
	setPermissionModel(t, "ownership")

	validator, _ := newVisaValidatorWithUserinfoFailure(t)

	r := newTestRouter(t, nil, validator)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer opaque-token")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func newTestRouter(t *testing.T, db DatasetLookup, vv *visa.Validator) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	initCaches(t)

	r := gin.New()
	r.Use(TokenMiddleware(db, vv, audit.NoopLogger{}))
	r.GET("/test", func(c *gin.Context) {
		authCtx, _ := GetAuthContext(c)
		c.JSON(http.StatusOK, authResponse{
			Subject:  authCtx.Subject,
			Datasets: authCtx.Datasets,
			Owned:    authCtx.OwnedDatasets,
			Visa:     authCtx.VisaDatasets,
		})
	})

	return r
}

func initCaches(t *testing.T) {
	t.Helper()

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     10000,
		BufferItems: 64,
	})
	require.NoError(t, err)
	sessionCache = &SessionCache{cache: cache}

	tokenCacheInst, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     10000,
		BufferItems: 64,
	})
	require.NoError(t, err)
	tokenCache = &SessionCache{cache: tokenCacheInst}
}

func setupAuthAndToken(t *testing.T, subject string) (string, *rsa.PrivateKey) {
	t.Helper()

	priv, pub := newKeyPair(t)
	keyset := jwk.NewSet()
	require.NoError(t, keyset.AddKey(pub))

	auth = &Authenticator{Keyset: keyset}

	token := signJWT(t, priv, "https://issuer.example", subject, time.Now().Add(time.Hour))

	return token, priv
}

func doRequest(t *testing.T, r *gin.Engine, token string, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	return resp
}

func decodeAuthResponse(t *testing.T, body *bytes.Buffer) authResponse {
	t.Helper()

	var resp authResponse
	require.NoError(t, json.Unmarshal(body.Bytes(), &resp))

	return resp
}

func newVisaValidator(t *testing.T, datasetID, issuer string) *visa.Validator {
	t.Helper()

	validator, _ := newVisaValidatorWithUserinfoCounter(t, datasetID, issuer)

	return validator
}

func newVisaValidatorWithUserinfoCounter(t *testing.T, datasetID, issuer string) (*visa.Validator, *int32) {
	t.Helper()

	priv, pub := newKeyPair(t)
	jwksServer := newJWKSServer(t, pub)
	t.Cleanup(jwksServer.Close)

	visaJWT := signVisaJWT(t, priv, jwksServer.URL, "visa-kid", issuer, "user-123", visaClaim(datasetID), time.Now().Add(time.Hour))

	var calls int32
	userinfoServer := newUserinfoServer(t, []string{visaJWT}, http.StatusOK, &calls)
	t.Cleanup(userinfoServer.Close)

	checker := &fakeDatasetChecker{existing: map[string]bool{datasetID: true}}

	cfg := visa.ValidatorConfig{
		Source:             "userinfo",
		UserinfoURL:        userinfoServer.URL,
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   0,
	}

	validator, err := visa.NewValidator(cfg, []visa.TrustedIssuer{{ISS: issuer, JKU: jwksServer.URL}}, checker)
	require.NoError(t, err)

	return validator, &calls
}

func newVisaValidatorWithUserinfoFailure(t *testing.T) (*visa.Validator, *int32) {
	t.Helper()

	var calls int32
	userinfoServer := newUserinfoServer(t, nil, http.StatusInternalServerError, &calls)
	t.Cleanup(userinfoServer.Close)

	checker := &fakeDatasetChecker{existing: map[string]bool{}}
	cfg := visa.ValidatorConfig{
		Source:             "userinfo",
		UserinfoURL:        userinfoServer.URL,
		DatasetIDMode:      "raw",
		IdentityMode:       "broker-bound",
		ValidateAsserted:   true,
		ClockSkew:          0,
		MaxVisas:           200,
		MaxJWKSPerReq:      10,
		MaxVisaSize:        16 * 1024,
		JWKCacheTTL:        time.Minute,
		ValidationCacheTTL: time.Minute,
		UserinfoCacheTTL:   time.Minute,
	}

	validator, err := visa.NewValidator(cfg, nil, checker)
	require.NoError(t, err)

	return validator, &calls
}

func newUserinfoServer(t *testing.T, passports []string, status int, calls *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			return
		}
		payload, _ := json.Marshal(map[string]any{
			"sub":               "user-123",
			"ga4gh_passport_v1": passports,
		})
		_, _ = w.Write(payload)
	}))
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

func visaClaim(dataset string) map[string]any {
	return map[string]any{
		"type":       "ControlledAccessGrants",
		"by":         "https://example.org/issuer",
		"value":      dataset,
		"source":     "https://example.org/source",
		"asserted":   time.Now().Add(-1 * time.Hour).Unix(),
		"conditions": []string{},
	}
}

func signVisaJWT(t *testing.T, priv *rsa.PrivateKey, jku, kid, iss, sub string, visaClaim map[string]any, exp time.Time) string {
	t.Helper()

	token := jwt.New()
	require.NoError(t, token.Set(jwt.IssuerKey, iss))
	require.NoError(t, token.Set(jwt.SubjectKey, sub))
	require.NoError(t, token.Set(jwt.ExpirationKey, exp))
	require.NoError(t, token.Set(jwt.IssuedAtKey, time.Now().Add(-1*time.Minute)))
	require.NoError(t, token.Set(jwt.NotBeforeKey, time.Now().Add(-1*time.Minute)))
	require.NoError(t, token.Set("ga4gh_visa_v1", visaClaim))

	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jwk.KeyIDKey, kid))
	require.NoError(t, headers.Set("jku", jku))

	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, priv, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)

	return string(signed)
}
