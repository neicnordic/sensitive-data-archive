package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/oauth2-proxy/mockoidc"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/oauth2"
)

type OIDCTests struct {
	suite.Suite
	TempDir    string
	ECKeyFile  *os.File
	RSAKeyFile *os.File
	mockServer *mockoidc.MockOIDC
	OIDCConfig config.OIDCConfig
}

func TestOIDCTestSuite(t *testing.T) {
	suite.Run(t, new(OIDCTests))
}

func (ts *OIDCTests) SetupTest() {
	var err error
	ts.mockServer, err = mockoidc.Run()
	assert.NoError(ts.T(), err)

	// create an elixir config that has the needed endpoints set
	ts.OIDCConfig = config.OIDCConfig{
		ID:          ts.mockServer.ClientID,
		Provider:    ts.mockServer.Issuer(),
		RedirectURL: "http://redirect",
		Secret:      ts.mockServer.ClientSecret,
	}
}

func (ts *OIDCTests) TearDownTest() {
	err := ts.mockServer.Shutdown()
	assert.NoError(ts.T(), err)
}

func (ts *OIDCTests) TestGetOidcClient() {
	expectedEndpoint := oauth2.Endpoint{
		AuthURL:   ts.mockServer.AuthorizationEndpoint(),
		TokenURL:  ts.mockServer.TokenEndpoint(),
		AuthStyle: 0}

	oauth2Config, provider := getOidcClient(ts.OIDCConfig)
	assert.Equal(ts.T(), ts.mockServer.ClientID, oauth2Config.ClientID, "ClientID was modified when creating the oauth2Config")
	assert.Equal(ts.T(), ts.mockServer.ClientSecret, oauth2Config.ClientSecret, "ClientSecret was modified when creating the oauth2Config")
	assert.Equal(ts.T(), ts.OIDCConfig.RedirectURL, oauth2Config.RedirectURL, "RedirectURL was modified when creating the oauth2Config")
	assert.Equal(ts.T(), expectedEndpoint, oauth2Config.Endpoint, "Issuer was modified when creating the oauth2Config")
	assert.Equal(ts.T(), expectedEndpoint, provider.Endpoint(), "provider has the wrong endpoint")
	assert.Equal(ts.T(), []string{"openid", "ga4gh_passport_v1 profile email eduperson_entitlement"}, oauth2Config.Scopes, "oauth2Config has the wrong scopes")
}

func (ts *OIDCTests) TestAuthenticateWithOidc() {
	// Create a code to authenticate
	session, err := ts.mockServer.SessionStore.NewSession(
		"openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	if err != nil {
		log.Error(err)
	}
	code := session.SessionID
	jwkURL := ts.mockServer.JWKSEndpoint()

	oauth2Config, provider := getOidcClient(ts.OIDCConfig)

	elixirIdentity, err := authenticateWithOidc(oauth2Config, provider, code, jwkURL)
	assert.Nil(ts.T(), err, "Failed to authenticate with OIDC")
	// Ensure both RawToken and ResignedToken are not empty
	assert.NotEqual(ts.T(), "", elixirIdentity.RawToken, "Empty RawToken returned from OIDC authentication")
	assert.NotEqual(ts.T(), "", elixirIdentity.ResignedToken, "Empty ResignedToken returned from OIDC authentication")
}

func (ts *OIDCTests) TestValidateJwt() {
	session, err := ts.mockServer.SessionStore.NewSession("openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	assert.NoError(ts.T(), err)
	oauth2Config, provider := getOidcClient(ts.OIDCConfig)
	jwkURL := ts.mockServer.JWKSEndpoint()
	elixirIdentity, _ := authenticateWithOidc(oauth2Config, provider, session.SessionID, jwkURL)
	elixirJWT := elixirIdentity.RawToken

	claims := map[string]any{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}
	token := jwt.New()
	for key, value := range claims {
		assert.NoError(ts.T(), token.Set(key, value))
	}

	// key from mock server
	suiteKey, err := jwk.FromRaw(ts.mockServer.Keypair.PrivateKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), suiteKey.Set(jwk.KeyIDKey, ts.mockServer.Keypair.Kid))

	// Create HS256 test token
	mySigningKey := []byte("AllYourBase")
	testTokenHS256, err := jwt.Sign(token, jwt.WithKey(jwa.HS256, mySigningKey))
	assert.NoError(ts.T(), err)

	// Create RSA test token
	rsaRawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(ts.T(), err)
	rsaKey, err := jwk.FromRaw(rsaRawKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), jwk.AssignKeyID(rsaKey))
	testTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, rsaKey))
	assert.NoError(ts.T(), err)

	// Create ECDSA test token
	ecRawKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(ts.T(), err)
	ecKey, err := jwk.FromRaw(ecRawKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), jwk.AssignKeyID(ecKey))
	testTokenEC, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, ecKey))
	assert.NoError(ts.T(), err)

	// Create expired RSA test token
	expiredTokenRSA := jwt.New()
	for key, value := range claims {
		assert.NoError(ts.T(), token.Set(key, value))
	}
	assert.NoError(ts.T(), expiredTokenRSA.Set(jwt.ExpirationKey, time.Now().UTC().Add(-4*time.Hour)))
	testExpiredTokenRSA, err := jwt.Sign(expiredTokenRSA, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(ts.T(), err)

	// sanity check
	_, expDate, err := validateToken(elixirJWT, ts.mockServer.JWKSEndpoint())
	assert.Nil(ts.T(), err)
	assert.Equal(ts.T(), expDate, elixirIdentity.ExpDateRaw, "Returned wrong exp date but without returning errors")

	// Not a jwk url
	_, _, err = validateToken(elixirJWT, "http://some/jwk/endpoint")
	assert.ErrorContains(ts.T(), err, "failed to fetch \"http://some/jwk/endpoint")

	// correct private key, RSA
	oidcTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(ts.T(), err)
	_, _, err = validateToken(string(oidcTokenRSA), ts.mockServer.JWKSEndpoint())
	assert.NoError(ts.T(), err)

	// wrong signing method
	_, _, err = validateToken(string(testTokenHS256), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, RSA
	_, _, err = validateToken(string(testTokenRSA), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, ECDSA
	_, _, err = validateToken(string(testTokenEC), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: key provider 0 failed:")

	// expired token
	_, _, err = validateToken(string(testExpiredTokenRSA), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: \"exp\" not satisfied")

	// check that we handle the case where token has no expiration date
	assert.NoError(ts.T(), token.Set(jwt.ExpirationKey, 0))
	noExpiryToken, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(ts.T(), err)
	_, _, err = validateToken(string(noExpiryToken), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: iitr between exp and iat is less than")

	// check that we handle the case where token has no exp key
	assert.NoError(ts.T(), token.Remove(jwt.ExpirationKey))
	noExpiryToken, err = jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(ts.T(), err)
	_, _, err = validateToken(string(noExpiryToken), ts.mockServer.JWKSEndpoint())
	assert.ErrorContains(ts.T(), err, "signed token not valid: \"exp\" not satisfied: required claim not found")
}
