package userauth

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"github.com/stretchr/testify/suite"

	"crypto/rand"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/minio/minio-go/v6/pkg/signer"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/stretchr/testify/assert"
)

var OIDCport int

type UserAuthTest struct {
	suite.Suite
}

func TestUserAuthTestSuite(t *testing.T) {
	suite.Run(t, new(UserAuthTest))
}

func (ts *UserAuthTest) SetupTest() {
}

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}
	_, b, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(b), "../../../")

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	oidc, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "python",
		Tag:        "3.10-slim",
		Cmd: []string{
			"/bin/sh",
			"-c",
			"pip install --upgrade pip && pip install aiohttp Authlib joserfc requests && python -u /oidc.py",
		},
		ExposedPorts: []string{"8080"},
		Mounts: []string{
			fmt.Sprintf("%s/.github/integration/sda/oidc.py:/oidc.py", rootDir),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	OIDCport, _ = strconv.Atoi(oidc.GetPort("8080/tcp"))
	oidcHostAndPort := oidc.GetHostPort("8080/tcp")

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+oidcHostAndPort+"/jwk", http.NoBody)
	if err != nil {
		log.Panic(err)
	}

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		_ = res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(oidc); err != nil {
			log.Panicf("Could not purge oidc resource: %s", err)
		}
		log.Panicf("Could not connect to oidc: %s", err)
	}

	log.Println("starting tests")
	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(oidc); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(0)
}

func (ts *UserAuthTest) TestAlwaysAuthenticator() {
	a := helper.NewAlwaysAllow()
	r, _ := http.NewRequest("Get", "/", nil)
	_, err := a.Authenticate(r)
	assert.Nil(ts.T(), err)
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_NoFile() {
	a := NewValidateFromToken(jwk.NewSet())
	err := a.ReadJwtPubKeyPath("")
	assert.Error(ts.T(), err)
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_GetFile() {
	demoKeysPath := "temp-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	err = a.ReadJwtPubKeyPath(jwtpubkeypath)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), 2, a.Keyset.Len())

	defer os.RemoveAll(demoKeysPath)
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_WrongURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := "/dummy/"

	err := a.FetchJwtPubKeyURL(jwtpubkeyurl)
	assert.Equal(ts.T(), "jwtpubkeyurl is not a proper URL (/dummy/)", err.Error())
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_BadURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := "dummy.com/jwk"

	err := a.FetchJwtPubKeyURL(jwtpubkeyurl)
	assert.Equal(ts.T(), "parse \"dummy.com/jwk\": invalid URI for request", err.Error())
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_GoodURL() {
	a := NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := fmt.Sprintf("http://localhost:%d/jwk", OIDCport)

	err := a.FetchJwtPubKeyURL(jwtpubkeyurl)
	assert.NoError(ts.T(), err, "failed to fetch remote JWK")
	assert.Equal(ts.T(), 3, a.Keyset.Len())
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_RSA() {
	// These tests should be possible to reuse with all correct authenticators somehow

	// Create temp demo rsa key pair
	demoKeysPath := "demo-rsa-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"
	a := NewValidateFromToken(jwk.NewSet())
	_ = a.ReadJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateRSAKey(prKeyPath, "/rsa")
	assert.NoError(ts.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(ts.T(), err)

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"

	// Test error from non-JWT token
	r.Header.Set("X-Amz-Security-Token", "notJWT")
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(ts.T(), err)

	r.Header.Set("X-Amz-Security-Token", defaultToken)

	// Test that a correct token works
	r.URL.Path = "/dummy/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	token, err := a.Authenticate(r)
	assert.NoError(ts.T(), err)
	privateClaims := token.PrivateClaims()
	assert.Equal(ts.T(), privateClaims["pilot"], helper.DefaultTokenClaims["pilot"])

	// Create and test expired Elixir token
	expiredToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.ExpiredClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", expiredToken)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(ts.T(), err)

	// Elixir token is not valid (e.g. issued in a future time)
	nonValidToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.NonValidClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", nonValidToken)
	r.URL.Path = "/username/"
	_, nonvalidToken := a.Authenticate(r)
	// The error output is huge so a smaller part is compared
	assert.Equal(ts.T(), "\"iat\" not satisfied", nonvalidToken.Error())

	// Elixir tokens broken
	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken[3:])
	r.URL.Path = "/username/"
	_, brokenToken := a.Authenticate(r)
	assert.Contains(ts.T(), brokenToken.Error(), "failed to parse jws: failed to parse JOSE headers:")

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", "random"+defaultToken)
	r.URL.Path = "/username/"
	_, err = a.Authenticate(r)
	assert.Error(ts.T(), err)
	_, brokenToken2 := a.Authenticate(r)
	assert.Contains(ts.T(), brokenToken2.Error(), "failed to parse jws: failed to parse JOSE headers:")

	// Bad issuer
	basIss, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.WrongTokenAlgClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", basIss)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Contains(ts.T(), err.Error(), "failed to get issuer from token")

	// Delete the keys when testing is done or failed
	defer os.RemoveAll(demoKeysPath)
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_EC() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-ec-keys"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.ReadJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateECKey(prKeyPath, "/ec")
	assert.NoError(ts.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.DefaultTokenClaims)
	assert.NoError(ts.T(), err)

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken)

	// Test that a correct token works
	r.URL.Path = "/dummy/"
	signer.SignV4(*r, "username", "testpass", "", "us-east-1")
	_, err = a.Authenticate(r)
	assert.Nil(ts.T(), err)

	// Create and test expired Elixir token
	expiredToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.ExpiredClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", expiredToken)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Error(ts.T(), err)

	// Elixir token is not valid
	nonValidToken, err := helper.CreateECToken(prKeyParsed, "ES256", helper.NonValidClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", nonValidToken)
	r.URL.Path = "/username/"
	_, nonvalidToken := a.Authenticate(r)
	// The error output is huge so a smaller part is compared
	assert.Equal(ts.T(), "\"iat\" not satisfied", nonvalidToken.Error())

	// Elixir tokens broken
	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", defaultToken[3:])
	r.URL.Path = "/username/"
	_, brokenToken := a.Authenticate(r)
	assert.Contains(ts.T(), brokenToken.Error(), "failed to parse jws: failed to parse JOSE headers:")

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", "random"+defaultToken)
	r.URL.Path = "/username/"
	_, brokenToken2 := a.Authenticate(r)
	assert.Contains(ts.T(), brokenToken2.Error(), "failed to parse jws: failed to parse JOSE headers:")

	// Bad issuer
	basIss, err := helper.CreateECToken(prKeyParsed, "ES256", helper.WrongTokenAlgClaims)
	assert.NoError(ts.T(), err)

	r, _ = http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", basIss)
	r.URL.Path = "/dummy/"
	_, err = a.Authenticate(r)
	assert.Contains(ts.T(), err.Error(), "failed to get issuer from token")

	defer os.RemoveAll(demoKeysPath)
}

func (ts *UserAuthTest) TestWrongKeyType_RSA() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-ec-keys"
	demoPrKeyName := "/ec"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateECkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.ReadJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	_, err = helper.ParsePrivateRSAKey(prKeyPath, demoPrKeyName)
	assert.Equal(ts.T(), "bad key format, expected RSA got EC", err.Error())

	defer os.RemoveAll(demoKeysPath)
}

func (ts *UserAuthTest) TestWrongKeyType_EC() {
	// Create temp demo ec key pair
	demoKeysPath := "demo-rsa-keys"
	demoPrKeyName := "/rsa"
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(ts.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(ts.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"

	a := NewValidateFromToken(jwk.NewSet())
	_ = a.ReadJwtPubKeyPath(jwtpubkeypath)

	// Parse demo private key
	_, err = helper.ParsePrivateECKey(prKeyPath, demoPrKeyName)
	assert.Equal(ts.T(), "bad key format, expected EC got RSA", err.Error())

	defer os.RemoveAll(demoKeysPath)
}

func (ts *UserAuthTest) TestUserTokenAuthenticator_ValidateSignature_HS() {
	// Create random secret
	key := make([]byte, 256)
	_, err := rand.Read(key)
	assert.NoError(ts.T(), err)

	// Create HS256 token
	wrongAlgToken, err := helper.CreateHSToken(key, helper.DefaultTokenClaims)
	assert.NoError(ts.T(), err)

	a := NewValidateFromToken(jwk.NewSet())

	r, _ := http.NewRequest("", "/", nil)
	r.Host = "localhost"
	r.Header.Set("X-Amz-Security-Token", wrongAlgToken)
	r.URL.Path = "/username/"
	_, wrongAlg := a.Authenticate(r)
	assert.Contains(ts.T(), wrongAlg.Error(), "failed to find key with key ID")
}

func TestGetBearerToken(t *testing.T) {
	authHeader := "Bearer sometoken"
	_, err := readTokenFromHeader(authHeader)
	assert.NoError(t, err)

	authHeader = "Bearer "
	_, err = readTokenFromHeader(authHeader)
	assert.EqualError(t, err, "token string is missing from authorization header")

	authHeader = "Beare"
	_, err = readTokenFromHeader(authHeader)
	assert.EqualError(t, err, "authorization scheme must be bearer")

	authHeader = ""
	_, err = readTokenFromHeader(authHeader)
	assert.EqualError(t, err, "authorization scheme must be bearer")
}
