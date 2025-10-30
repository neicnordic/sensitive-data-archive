package authenticator

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/suite"
)

type AuthenticatorTestSuite struct {
	suite.Suite

	tempDir   string
	ginEngine *gin.Engine

	token []byte

	httpTestServer *httptest.Server
}

func (ts *AuthenticatorTestSuite) SetupTest() {
	ts.tempDir = ts.T().TempDir()
	ts.ginEngine = gin.Default()

	privateKeyRaw, publicKeyRaw, err := generateEs256PemKey()
	if err != nil {
		ts.FailNow(err.Error())
	}

	pubKeyPath := filepath.Join(ts.tempDir, "jwt.pub")
	if err := os.WriteFile(pubKeyPath, publicKeyRaw, 0600); err != nil {
		ts.FailNow("failed to write PEM key to PEM", err)
	}

	ts.token, err = generateToken(privateKeyRaw)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.httpTestServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		keySet := jwk.NewSet()

		publicKey, err := jwk.ParseKey(publicKeyRaw, jwk.WithPEM(true))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := keySet.AddKey(publicKey); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		raw, err := json.Marshal(keySet)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		// Set the response body

		_, _ = fmt.Fprint(w, string(raw))
	}))
}

func generateToken(privateKeyRaw []byte) ([]byte, error) {
	token := jwt.New()
	token.Set("sub", "test_user")
	token.Set("iss", "http://unit.test")
	token.Set("exp", time.Now().Add(time.Second*30).Unix())
	token.Set("iat", time.Now().Unix())

	prKeyParsed, err := jwk.ParseKey(privateKeyRaw, jwk.WithPEM(true))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}
	if err := jwk.AssignKeyID(prKeyParsed); err != nil {
		return nil, fmt.Errorf("failed to assign key id: %v", err)
	}
	tokenRaw, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, prKeyParsed))
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %v", err)
	}
	return tokenRaw, nil
}

func generateEs256PemKey() ([]byte, []byte, error) {

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %v", err)
	}
	publickey := &key.PublicKey

	// dump private key to file
	privateKeyBytes, err := jwk.EncodePEM(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode private key to PEM: %v", err)
	}

	// dump public key to file
	publicKeyBytes, err := jwk.EncodePEM(publickey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode public key to PEM: %v", err)
	}

	return privateKeyBytes, publicKeyBytes, nil
}

func (ts *AuthenticatorTestSuite) TearDownTest() {
	ts.httpTestServer.Close()
}
func TestValidatorAPITestSuite(t *testing.T) {
	suite.Run(t, new(AuthenticatorTestSuite))
}

func (ts *AuthenticatorTestSuite) TestAuthenticator_FromJwtPubKeyFile() {
	newAuthenticator, err := NewAuthenticator(
		JwtPubKeyPath(ts.tempDir),
	)

	if err != nil {
		ts.FailNow("failed to create new authenticator", err)
	}

	ts.ginEngine.Use(newAuthenticator.Authenticate())

	ts.ginEngine.GET("/", func(c *gin.Context) {
		if _, ok := c.Get("token"); !ok {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.AbortWithStatus(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)

	req.Header.Add("Authorization", "Bearer "+string(ts.token))

	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)
}

func (ts *AuthenticatorTestSuite) TestAuthenticator_FromJwtPubKeyUrl() {

	newAuthenticator, err := NewAuthenticator(
		JwtPubKeyUrl(ts.httpTestServer.URL),
	)

	if err != nil {
		ts.FailNow("failed to create new authenticator", err)
	}

	ts.ginEngine.Use(newAuthenticator.Authenticate())

	ts.ginEngine.GET("/", func(c *gin.Context) {
		if _, ok := c.Get("token"); !ok {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.AbortWithStatus(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)

	req.Header.Add("Authorization", "Bearer "+string(ts.token))

	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)
}

func (ts *AuthenticatorTestSuite) TestAuthenticator_FromJwtPubKeyUrl_TokenSignedWithDifferentKey() {

	newAuthenticator, err := NewAuthenticator(
		JwtPubKeyUrl(ts.httpTestServer.URL),
	)

	if err != nil {
		ts.FailNow("failed to create new authenticator", err)
	}

	ts.ginEngine.Use(newAuthenticator.Authenticate())

	ts.ginEngine.GET("/", func(c *gin.Context) {
		if _, ok := c.Get("token"); !ok {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.AbortWithStatus(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)

	privateKeyRaw, _, err := generateEs256PemKey()
	if err != nil {
		ts.FailNow(err.Error())
	}

	tokenWithDifferentKey, err := generateToken(privateKeyRaw)
	if err != nil {
		ts.FailNow(err.Error())
	}
	req.Header.Add("Authorization", "Bearer "+string(tokenWithDifferentKey))

	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusUnauthorized, w.Code)
}

func (ts *AuthenticatorTestSuite) TestAuthenticator_FromJwtPubKeyPath_TokenSignedWithDifferentKey() {
	newAuthenticator, err := NewAuthenticator(
		JwtPubKeyPath(ts.tempDir),
	)
	if err != nil {
		ts.FailNow("failed to create new authenticator", err)
	}

	ts.ginEngine.Use(newAuthenticator.Authenticate())

	ts.ginEngine.GET("/", func(c *gin.Context) {
		if _, ok := c.Get("token"); !ok {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.AbortWithStatus(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)

	privateKeyRaw, _, err := generateEs256PemKey()
	if err != nil {
		ts.FailNow(err.Error())
	}

	tokenWithDifferentKey, err := generateToken(privateKeyRaw)
	if err != nil {
		ts.FailNow(err.Error())
	}
	req.Header.Add("Authorization", "Bearer "+string(tokenWithDifferentKey))

	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusUnauthorized, w.Code)
}
