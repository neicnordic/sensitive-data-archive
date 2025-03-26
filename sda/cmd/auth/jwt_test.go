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

func (jwtts *JWTTests) SetupTest() {
	var err error
	jwtts.TempDir, err = os.MkdirTemp(os.TempDir(), "jwt-test")
	assert.NoError(jwtts.T(), err)

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(jwtts.T(), err)
	ecKeyBytes, err := jwk.EncodePEM(ecKey)
	assert.NoError(jwtts.T(), err)
	assert.NoError(jwtts.T(), os.WriteFile(jwtts.TempDir+"/ec", ecKeyBytes, 0600))

	ecPubKeyBytes, err := jwk.EncodePEM(&ecKey.PublicKey)
	assert.NoError(jwtts.T(), err)
	assert.NoError(jwtts.T(), os.WriteFile(jwtts.TempDir+"/ec.pub", ecPubKeyBytes, 0600))

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(jwtts.T(), err)

	rsaKeyBytes, err := jwk.EncodePEM(rsaKey)
	assert.NoError(jwtts.T(), err)
	assert.NoError(jwtts.T(), os.WriteFile(jwtts.TempDir+"/rsa", rsaKeyBytes, 0600))

	rsaPubKeyBytes, err := jwk.EncodePEM(&rsaKey.PublicKey)
	assert.NoError(jwtts.T(), err)
	assert.NoError(jwtts.T(), os.WriteFile(jwtts.TempDir+"/rsa.pub", rsaPubKeyBytes, 0600))
}

func (jwtts *JWTTests) TearDownTest() {
	os.RemoveAll(jwtts.TempDir)
}

func (jwtts *JWTTests) TestGenerateJwtToken() {
	type KeyAlgo struct {
		Algorithm string
		Keyfile   string
		Pubfile   string
	}

	algorithms := []KeyAlgo{
		{Algorithm: "RS256", Keyfile: jwtts.TempDir + "/rsa", Pubfile: jwtts.TempDir + "/rsa.pub"},
		{Algorithm: "ES256", Keyfile: jwtts.TempDir + "/ec", Pubfile: jwtts.TempDir + "/ec.pub"},
	}

	claims := map[string]interface{}{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}

	for _, test := range algorithms {
		ts, expiration, err := generateJwtToken(claims, test.Keyfile, test.Algorithm)
		assert.NoError(jwtts.T(), err)
		assert.NotNil(jwtts.T(), ts)

		keyData, err := os.ReadFile(test.Pubfile)
		assert.NoError(jwtts.T(), err)
		key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
		assert.NoError(jwtts.T(), err)
		assert.NoError(jwtts.T(), jwk.AssignKeyID(key))
		keySet := jwk.NewSet()
		assert.NoError(jwtts.T(), keySet.AddKey(key))

		token, err := jwt.Parse([]byte(ts), jwt.WithKeySet(keySet, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		assert.NoError(jwtts.T(), err)
		assert.Equal(jwtts.T(), "http://local.issuer", token.Issuer())
		assert.Equal(jwtts.T(), "test@foo.bar", token.Subject())

		// check that the expiration string is a date
		_, err = time.Parse("2006-01-02 15:04:05", expiration)
		assert.Nil(jwtts.T(), err, "Couldn't parse expiration date for jwt")
	}
}
