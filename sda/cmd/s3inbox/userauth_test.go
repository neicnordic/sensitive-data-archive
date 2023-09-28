package main

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"testing"

	helper "github.com/neicnordic/sensitive-data-archive/internal/helper"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/minio/minio-go/v6/pkg/signer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type UserAuthTest struct {
	suite.Suite
}
func TestUserAuthTestSuite(t *testing.T) {
	suite.Run(t, new(UserAuthTest))
}

func (suite *UserAuthTest) SetupTest() {

}

// AlwaysAllow is an Authenticator that always authenticates
type AlwaysAllow struct{}

// NewAlwaysAllow returns a new AlwaysAllow authenticator.
func NewAlwaysAllow() *AlwaysAllow {
	return &AlwaysAllow{}
}

// Authenticate authenticates everyone.
func (u *AlwaysAllow) Authenticate(_ *http.Request) (jwt.Token, error) {
	return jwt.New(), nil
}

func (suite *UserAuthTest) TestAlwaysAuthenticator() {
	a := NewAlwaysAllow()
	r, _ := http.NewRequest("Get", "/", nil)
	_, err := a.Authenticate(r)
	assert.Nil(suite.T(), err)
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_NoFile() {
	a := NewValidateFromToken(jwk.NewSet())
	err := a.readJwtPubKeyPath("")
	assert.Error(suite.T(), err)
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_GetFile() {
	demoKeysPath := "temp-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	err = a.readJwtPubKeyPath(jwtpubkeypath)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), 2, a.keyset.Len())

	defer os.RemoveAll(demoKeysPath)
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_WrongURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := "/dummy/"

	err := a.fetchJwtPubKeyURL(jwtpubkeyurl)
	assert.Equal(suite.T(), "jwtpubkeyurl is not a proper URL (/dummy/)", err.Error())
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_BadURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := "dummy.com/jwk"

	err := a.fetchJwtPubKeyURL(jwtpubkeyurl)
	assert.Equal(suite.T(), "parse \"dummy.com/jwk\": invalid URI for request", err.Error())
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_GoodURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := fmt.Sprintf("http://localhost:%d/jwk", OIDCport)

	err := a.fetchJwtPubKeyURL(jwtpubkeyurl)
	assert.NoError(suite.T(), err, "failed to fetch remote JWK")
	assert.Equal(suite.T(), 3, a.keyset.Len())
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_RSA() {
	// These tests should be possible to reuse with all correct authenticators somehow

	// Create temp demo rsa key pair
	demoKeysPath := "demo-rsa-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"
	a := NewValidateFromToken(jwk.NewSet())
	_ = a.readJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateRSAKey(prKeyPath, "/rsa")
	assert.NoError(suite.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"

	// Test error from non-JWT token
	r.Header.Set("X-Amz-Security-Token", "notJWT")
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(suite.T(), err)

	r.Header.Set("X-Amz-Security-Token", defaultToken)

	// Test that a user can access their own bucket
	r.URL.Path = "/dummy/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	token, err := a.Authenticate(r)
	assert.NoError(suite.T(), err)
	privateClaims := token.PrivateClaims()
	assert.Equal(suite.T(), privateClaims["pilot"], helper.DefaultTokenClaims["pilot"])

	// Test that an unexpected path gives an error
	r.URL.Path = "error"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	_, err = a.Authenticate(r)
	assert.Error(suite.T(), err)

	// Test that a valid user can't access someone elses bucket
	r.URL.Path = "/notvalid/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	_, otherBucket := a.Authenticate(r)
	assert.Equal(suite.T(), "token supplied username dummy but URL had notvalid", otherBucket.Error())

	// Create and test Elixir token with wrong username
	wrongUserToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.WrongUserClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", wrongUserToken)
	r.URL.Path = "/username/"
	_, wrongUsername := a.Authenticate(r)
	assert.Equal(suite.T(), "token supplied username c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org but URL had username", wrongUsername.Error())

	// Create and test expired Elixir token
	expiredToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.ExpiredClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", expiredToken)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(suite.T(), err)

	// Elixir token is not valid (e.g. issued in a future time)
	nonValidToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.NonValidClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", nonValidToken)
	r.URL.Path = "/username/"
	_, nonvalidToken := a.Authenticate(r)
	// The error output is huge so a smaller part is compared
	assert.Equal(suite.T(), "signed token not valid: \"iat\" not satisfied", nonvalidToken.Error()[0:43])

	// Elixir tokens broken
	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken[3:])
	r.URL.Path = "/username/"
	_, brokenToken := a.Authenticate(r)
	assert.Equal(suite.T(), "signed token not valid: failed to parse jws: failed to parse JOSE headers:", brokenToken.Error()[0:74])

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", "random"+defaultToken)
	r.URL.Path = "/username/"
	_, err = a.Authenticate(r)
	assert.Error(suite.T(), err)
	_, brokenToken2 := a.Authenticate(r)
	assert.Equal(suite.T(), "signed token not valid: failed to parse jws: failed to parse JOSE headers:", brokenToken2.Error()[0:74])

	// Bad issuer
	basIss, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.WrongTokenAlgClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", basIss)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Contains(suite.T(), err.Error(), "failed to get issuer from token")

	// Delete the keys when testing is done or failed
	defer os.RemoveAll(demoKeysPath)
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_EC() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-ec-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.readJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateECKey(prKeyPath, "/ec")
	assert.NoError(suite.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken)

	// Test that a user can access their own bucket
	r.URL.Path = "/dummy/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	_, err = a.Authenticate(r)
	assert.Nil(suite.T(), err)

	// Test that a valid user can't access someone elses bucket
	r.URL.Path = "/notvalid/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	_, otherBucket := a.Authenticate(r)
	assert.Equal(suite.T(), "token supplied username dummy but URL had notvalid", otherBucket.Error())

	// Create and test Elixir token with wrong username
	wrongUserToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.WrongUserClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", wrongUserToken)
	r.URL.Path = "/username/"
	_, wrongUsername := a.Authenticate(r)
	assert.Equal(suite.T(), "token supplied username c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org but URL had username", wrongUsername.Error())

	// Create and test expired Elixir token
	expiredToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.ExpiredClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", expiredToken)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(suite.T(), err)

	// Elixir token is not valid
	nonValidToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.NonValidClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", nonValidToken)
	r.URL.Path = "/username/"
	_, nonvalidToken := a.Authenticate(r)
	// The error output is huge so a smaller part is compared
	assert.Equal(suite.T(), "signed token not valid: \"iat\" not satisfied", nonvalidToken.Error()[0:43])

	// Elixir tokens broken
	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken[3:])
	r.URL.Path = "/username/"
	_, brokenToken := a.Authenticate(r)
	assert.Equal(suite.T(), "signed token not valid: failed to parse jws: failed to parse JOSE headers:", brokenToken.Error()[0:74])

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", "random"+defaultToken)
	r.URL.Path = "/username/"
	_, brokenToken2 := a.Authenticate(r)
	assert.Equal(suite.T(), "signed token not valid: failed to parse jws: failed to parse JOSE headers:", brokenToken2.Error()[0:74])

	// Bad issuer
	basIss, err := helper.CreateECToken(prKeyParsed, "ES256", helper.WrongTokenAlgClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", basIss)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Contains(suite.T(), err.Error(), "failed to get issuer from token")

	// Test LS-AAI user authentication
	token, err := helper.CreateECToken(prKeyParsed, "ES256", helper.WrongUserClaims)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/c5773f41d17d27bd53b1e6794aedc32d7906e779_elixir-europe.org/foo", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", token)
	_, err = a.Authenticate(r)
	assert.NoError(suite.T(), err)

	r, _ = http.NewRequest("", "/dataset", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", token)
	_, err = a.Authenticate(r)
	assert.Equal(suite.T(), "token supplied username c5773f41d17d27bd53b1e6794aedc32d7906e779@elixir-europe.org but URL had dataset", err.Error())

	defer os.RemoveAll(demoKeysPath)
}

func (suite *UserAuthTest) TestWrongKeyType_RSA() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-ec-keys"
	demoPrKeyName := "/ec"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.readJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	_, err = helper.ParsePrivateRSAKey(prKeyPath, demoPrKeyName)
	assert.Equal(suite.T(), "bad key format, expected RSA got EC", err.Error())

	defer os.RemoveAll(demoKeysPath)
}

func (suite *UserAuthTest) TestWrongKeyType_EC() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-rsa-keys"
	demoPrKeyName := "/rsa"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.readJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	_, err = helper.ParsePrivateECKey(prKeyPath, demoPrKeyName)
	assert.Equal(suite.T(), "bad key format, expected EC got RSA", err.Error())

	defer os.RemoveAll(demoKeysPath)
}

func (suite *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_HS() {
	// Create random secret
	key := make([]byte, 256)
	_, err := rand.Read(key)
	assert.NoError(suite.T(), err)

	// Create HS256 token
	wrongAlgToken, err := helper.CreateHSToken(key, "HS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)

	a := NewValidateFromToken(jwk.NewSet())

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", wrongAlgToken)
	r.URL.Path = "/username/"
	_, WrongAlg := a.Authenticate(r)
	assert.Contains(suite.T(), WrongAlg.Error(), "signed token not valid:")
}
