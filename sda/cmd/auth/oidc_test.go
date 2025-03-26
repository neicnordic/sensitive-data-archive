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

func (ot *OIDCTests) SetupTest() {
	var err error
	ot.mockServer, err = mockoidc.Run()
	assert.NoError(ot.T(), err)

	// create an elixir config that has the needed endpoints set
	ot.OIDCConfig = config.OIDCConfig{
		ID:          ot.mockServer.ClientID,
		Provider:    ot.mockServer.Issuer(),
		RedirectURL: "http://redirect",
		Secret:      ot.mockServer.ClientSecret,
	}
}

func (ot *OIDCTests) TearDownTest() {
	err := ot.mockServer.Shutdown()
	assert.NoError(ot.T(), err)
}

func (ot *OIDCTests) TestGetOidcClient() {
	expectedEndpoint := oauth2.Endpoint{
		AuthURL:   ot.mockServer.AuthorizationEndpoint(),
		TokenURL:  ot.mockServer.TokenEndpoint(),
		AuthStyle: 0}

	oauth2Config, provider := getOidcClient(ot.OIDCConfig)
	assert.Equal(ot.T(), ot.mockServer.ClientID, oauth2Config.ClientID, "ClientID was modified when creating the oauth2Config")
	assert.Equal(ot.T(), ot.mockServer.ClientSecret, oauth2Config.ClientSecret, "ClientSecret was modified when creating the oauth2Config")
	assert.Equal(ot.T(), ot.OIDCConfig.RedirectURL, oauth2Config.RedirectURL, "RedirectURL was modified when creating the oauth2Config")
	assert.Equal(ot.T(), expectedEndpoint, oauth2Config.Endpoint, "Issuer was modified when creating the oauth2Config")
	assert.Equal(ot.T(), expectedEndpoint, provider.Endpoint(), "provider has the wrong endpoint")
	assert.Equal(ot.T(), []string{"openid", "ga4gh_passport_v1 profile email eduperson_entitlement"}, oauth2Config.Scopes, "oauth2Config has the wrong scopes")
}

func (ot *OIDCTests) TestAuthenticateWithOidc() {
	// Create a code to authenticate
	session, err := ot.mockServer.SessionStore.NewSession(
		"openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	if err != nil {
		log.Error(err)
	}
	code := session.SessionID
	jwkURL := ot.mockServer.JWKSEndpoint()

	oauth2Config, provider := getOidcClient(ot.OIDCConfig)

	elixirIdentity, err := authenticateWithOidc(oauth2Config, provider, code, jwkURL)
	assert.Nil(ot.T(), err, "Failed to authenticate with OIDC")
	// Ensure both RawToken and ResignedToken are not empty
	assert.NotEqual(ot.T(), "", elixirIdentity.RawToken, "Empty RawToken returned from OIDC authentication")
	assert.NotEqual(ot.T(), "", elixirIdentity.ResignedToken, "Empty ResignedToken returned from OIDC authentication")
}

func (ot *OIDCTests) TestValidateJwt() {
	session, err := ot.mockServer.SessionStore.NewSession("openid email profile", "nonce", mockoidc.DefaultUser(), "", "")
	assert.NoError(ot.T(), err)
	oauth2Config, provider := getOidcClient(ot.OIDCConfig)
	jwkURL := ot.mockServer.JWKSEndpoint()
	elixirIdentity, _ := authenticateWithOidc(oauth2Config, provider, session.SessionID, jwkURL)
	elixirJWT := elixirIdentity.RawToken

	claims := map[string]interface{}{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}
	token := jwt.New()
	for key, value := range claims {
		assert.NoError(ot.T(), token.Set(key, value))
	}

	// key from mock server
	otKey, err := jwk.FromRaw(ot.mockServer.Keypair.PrivateKey)
	assert.NoError(ot.T(), err)
	assert.NoError(ot.T(), otKey.Set(jwk.KeyIDKey, ot.mockServer.Keypair.Kid))

	// Create HS256 test token
	mySigningKey := []byte("AllYourBase")
	testTokenHS256, err := jwt.Sign(token, jwt.WithKey(jwa.HS256, mySigningKey))
	assert.NoError(ot.T(), err)

	// Create RSA test token
	rsaRawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(ot.T(), err)
	rsaKey, err := jwk.FromRaw(rsaRawKey)
	assert.NoError(ot.T(), err)
	assert.NoError(ot.T(), jwk.AssignKeyID(rsaKey))
	testTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, rsaKey))
	assert.NoError(ot.T(), err)

	// Create ECDSA test token
	ecRawKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(ot.T(), err)
	ecKey, err := jwk.FromRaw(ecRawKey)
	assert.NoError(ot.T(), err)
	assert.NoError(ot.T(), jwk.AssignKeyID(ecKey))
	testTokenEC, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, ecKey))
	assert.NoError(ot.T(), err)

	// Create expired RSA test token
	expiredTokenRSA := jwt.New()
	for key, value := range claims {
		assert.NoError(ot.T(), token.Set(key, value))
	}
	assert.NoError(ot.T(), expiredTokenRSA.Set(jwt.ExpirationKey, time.Now().UTC().Add(-4*time.Hour)))
	testExpiredTokenRSA, err := jwt.Sign(expiredTokenRSA, jwt.WithKey(jwa.RS256, otKey))
	assert.NoError(ot.T(), err)

	// sanity check
	_, expDate, err := validateToken(elixirJWT, ot.mockServer.JWKSEndpoint())
	assert.Nil(ot.T(), err)
	assert.Equal(ot.T(), expDate, elixirIdentity.ExpDateRaw, "Returned wrong exp date but without returning errors")

	// Not a jwk url
	_, _, err = validateToken(elixirJWT, "http://some/jwk/endpoint")
	assert.ErrorContains(ot.T(), err, "failed to fetch \"http://some/jwk/endpoint")

	// correct private key, RSA
	oidcTokenRSA, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, otKey))
	assert.NoError(ot.T(), err)
	_, _, err = validateToken(string(oidcTokenRSA), ot.mockServer.JWKSEndpoint())
	assert.NoError(ot.T(), err)

	// wrong signing method
	_, _, err = validateToken(string(testTokenHS256), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, RSA
	_, _, err = validateToken(string(testTokenRSA), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: key provider 0 failed:")

	// wrong private key, ECDSA
	_, _, err = validateToken(string(testTokenEC), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: key provider 0 failed:")

	// expired token
	_, _, err = validateToken(string(testExpiredTokenRSA), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: \"exp\" not satisfied")

	// check that we handle the case where token has no expiration date
	assert.NoError(ot.T(), token.Set(jwt.ExpirationKey, 0))
	noExpiryToken, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, otKey))
	assert.NoError(ot.T(), err)
	_, _, err = validateToken(string(noExpiryToken), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: iitr between exp and iat is less than")

	// check that we handle the case where token has no exp key
	assert.NoError(ot.T(), token.Remove(jwt.ExpirationKey))
	noExpiryToken, err = jwt.Sign(token, jwt.WithKey(jwa.RS256, otKey))
	assert.NoError(ot.T(), err)
	_, _, err = validateToken(string(noExpiryToken), ot.mockServer.JWKSEndpoint())
	assert.ErrorContains(ot.T(), err, "signed token not valid: \"exp\" not satisfied: required claim not found")
}
