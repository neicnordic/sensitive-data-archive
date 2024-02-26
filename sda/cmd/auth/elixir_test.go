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

type ElixirTests struct {
	suite.Suite
	TempDir      string
	ECKeyFile    *os.File
	RSAKeyFile   *os.File
	mockServer   *mockoidc.MockOIDC
	ElixirConfig config.ElixirConfig
}

func TestElixirTestSuite(t *testing.T) {
	suite.Run(t, new(ElixirTests))
}

func (suite *ElixirTests) SetupTest() {
	var err error
	suite.mockServer, err = mockoidc.Run()
	assert.NoError(suite.T(), err)

	// create an elixir config that has the needed endpoints set
	suite.ElixirConfig = config.ElixirConfig{
		ID:          suite.mockServer.ClientID,
		Provider:    suite.mockServer.Issuer(),
		RedirectURL: "http://redirect",
		Secret:      suite.mockServer.ClientSecret,
	}
}

func (suite *ElixirTests) TearDownTest() {
	err := suite.mockServer.Shutdown()
	assert.NoError(suite.T(), err)
}

func (suite *ElixirTests) TestGetOidcClient() {

	expectedEndpoint := oauth2.Endpoint{
		AuthURL:   suite.mockServer.AuthorizationEndpoint(),
		TokenURL:  suite.mockServer.TokenEndpoint(),
		AuthStyle: 0}

	oauth2Config, provider := getOidcClient(suite.ElixirConfig)
	assert.Equal(suite.T(), suite.mockServer.ClientID, oauth2Config.ClientID, "ClientID was modified when creating the oauth2Config")
	assert.Equal(suite.T(), suite.mockServer.ClientSecret, oauth2Config.ClientSecret, "ClientSecret was modified when creating the oauth2Config")
	assert.Equal(suite.T(), suite.ElixirConfig.RedirectURL, oauth2Config.RedirectURL, "RedirectURL was modified when creating the oauth2Config")
	assert.Equal(suite.T(), expectedEndpoint, oauth2Config.Endpoint, "Issuer was modified when creating the oauth2Config")
	assert.Equal(suite.T(), expectedEndpoint, provider.Endpoint(), "provider has the wrong endpoint")
	assert.Equal(suite.T(), []string{"openid", "ga4gh_passport_v1 profile email eduperson_entitlement"}, oauth2Config.Scopes, "oauth2Config has the wrong scopes")
}

func (suite *ElixirTests) TestAuthenticateWithOidc() {

	// Create a code to authenticate

	session, err := suite.mockServer.SessionStore.NewSession(
		"openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	if err != nil {
		log.Error(err)
	}
	code := session.SessionID
	jwkURL := suite.mockServer.JWKSEndpoint()

	oauth2Config, provider := getOidcClient(suite.ElixirConfig)

	elixirIdentity, err := authenticateWithOidc(oauth2Config, provider, code, jwkURL)
	assert.Nil(suite.T(), err, "Failed to authenticate with OIDC")
	assert.NotEqual(suite.T(), "", elixirIdentity.Token, "Empty token returned from OIDC authentication")
}

func (suite *ElixirTests) TestValidateJwt() {
	session, err := suite.mockServer.SessionStore.NewSession("openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	assert.NoError(suite.T(), err)
	oauth2Config, provider := getOidcClient(suite.ElixirConfig)
	jwkURL := suite.mockServer.JWKSEndpoint()
	elixirIdentity, _ := authenticateWithOidc(oauth2Config, provider, session.SessionID, jwkURL)
	elixirJWT := elixirIdentity.Token

	claims := map[string]interface{}{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}
	token := jwt.New()
	for key, value := range claims {
		assert.NoError(suite.T(), token.Set(key, value))
	}

	// key from mock server
	suiteKey, err := jwk.FromRaw(suite.mockServer.Keypair.PrivateKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), suiteKey.Set(jwk.KeyIDKey, suite.mockServer.Keypair.Kid))

	// Create HS256 test token
	mySigningKey := []byte("AllYourBase")
	testTokenHS256, err := jwt.Sign(token, jwt.WithKey(jwa.HS256, mySigningKey))
	assert.NoError(suite.T(), err)

	// Create RSA test token
	rsaRawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(suite.T(), err)
	rsaKey, err := jwk.FromRaw(rsaRawKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), jwk.AssignKeyID(rsaKey))
	testTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, rsaKey))
	assert.NoError(suite.T(), err)

	// Create ECDSA test token
	ecRawKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(suite.T(), err)
	ecKey, err := jwk.FromRaw(ecRawKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), jwk.AssignKeyID(ecKey))
	testTokenEC, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, ecKey))
	assert.NoError(suite.T(), err)

	// Create expired RSA test token
	expiredTokenRSA := jwt.New()
	for key, value := range claims {
		assert.NoError(suite.T(), token.Set(key, value))
	}
	assert.NoError(suite.T(), expiredTokenRSA.Set(jwt.ExpirationKey, time.Now().UTC().Add(-4*time.Hour)))
	testExpiredTokenRSA, err := jwt.Sign(expiredTokenRSA, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(suite.T(), err)

	// sanity check
	_, expDate, err := validateToken(elixirJWT, suite.mockServer.JWKSEndpoint())
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), expDate, elixirIdentity.ExpDate, "Returned wrong exp date but without returning errors")

	// Not a jwk url
	_, _, err = validateToken(elixirJWT, "http://some/jwk/endpoint")
	assert.ErrorContains(suite.T(), err, "failed to fetch \"http://some/jwk/endpoint")

	// correct private key, RSA
	oidcTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(suite.T(), err)
	_, _, err = validateToken(string(oidcTokenRSA), suite.mockServer.JWKSEndpoint())
	assert.NoError(suite.T(), err)

	// wrong signing method
	_, _, err = validateToken(string(testTokenHS256), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, RSA
	_, _, err = validateToken(string(testTokenRSA), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, ECDSA
	_, _, err = validateToken(string(testTokenEC), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: key provider 0 failed:")

	// expired token
	_, _, err = validateToken(string(testExpiredTokenRSA), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: \"exp\" not satisfied")

	// check that we handle the case where token has no expiration date
	assert.NoError(suite.T(), token.Set(jwt.ExpirationKey, 0))
	noExpiryToken, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(suite.T(), err)
	_, _, err = validateToken(string(noExpiryToken), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: iitr between exp and iat is less than")

	// check that we handle the case where token has no exp key
	assert.NoError(suite.T(), token.Remove(jwt.ExpirationKey))
	noExpiryToken, err = jwt.Sign(token, jwt.WithKey(jwa.RS256, suiteKey))
	assert.NoError(suite.T(), err)
	_, _, err = validateToken(string(noExpiryToken), suite.mockServer.JWKSEndpoint())
	assert.ErrorContains(suite.T(), err, "signed token not valid: \"exp\" not satisfied: required claim not found")
}
