package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type JWTTests struct {
	suite.Suite
	TempDir string
}

func TestJWTTestSuite(t *testing.T) {
	suite.Run(t, new(JWTTests))
}

func (suite *JWTTests) SetupTest() {
	var err error
	suite.TempDir, err = os.MkdirTemp(os.TempDir(), "jwt-test")
	assert.NoError(suite.T(), err)

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(suite.T(), err)
	ecKeyBytes, err := jwk.EncodePEM(ecKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), os.WriteFile(suite.TempDir+"/ec", ecKeyBytes, 0600))

	ecPubKeyBytes, err := jwk.EncodePEM(&ecKey.PublicKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), os.WriteFile(suite.TempDir+"/ec.pub", ecPubKeyBytes, 0600))

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(suite.T(), err)

	rsaKeyBytes, err := jwk.EncodePEM(rsaKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), os.WriteFile(suite.TempDir+"/rsa", rsaKeyBytes, 0600))

	rsaPubKeyBytes, err := jwk.EncodePEM(&rsaKey.PublicKey)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), os.WriteFile(suite.TempDir+"/rsa.pub", rsaPubKeyBytes, 0600))
}

func (suite *JWTTests) TearDownTest() {
	os.RemoveAll(suite.TempDir)
}

func (suite *JWTTests) TestGenerateJwtToken() {
	type KeyAlgo struct {
		Algorithm string
		Keyfile   string
		Pubfile   string
	}

	algorithms := []KeyAlgo{
		{Algorithm: "RS256", Keyfile: suite.TempDir + "/rsa", Pubfile: suite.TempDir + "/rsa.pub"},
		{Algorithm: "ES256", Keyfile: suite.TempDir + "/ec", Pubfile: suite.TempDir + "/ec.pub"},
	}

	claims := map[string]interface{}{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}

	for _, test := range algorithms {
		ts, expiration, err := generateJwtToken(claims, test.Keyfile, test.Algorithm)
		assert.NoError(suite.T(), err)
		assert.NotNil(suite.T(), ts)

		keyData, err := os.ReadFile(test.Pubfile)
		assert.NoError(suite.T(), err)
		key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
		assert.NoError(suite.T(), err)
		assert.NoError(suite.T(), jwk.AssignKeyID(key))
		keySet := jwk.NewSet()
		assert.NoError(suite.T(), keySet.AddKey(key))

		token, err := jwt.Parse([]byte(ts), jwt.WithKeySet(keySet, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		assert.NoError(suite.T(), err)
		assert.Equal(suite.T(), "http://local.issuer", token.Issuer())
		assert.Equal(suite.T(), "test@foo.bar", token.Subject())

		// check that the expiration string is a date
		_, err = time.Parse("2006-01-02 15:04:05", expiration)
		assert.Nil(suite.T(), err, "Couldn't parse expiration date for jwt")
	}
}
