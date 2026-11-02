package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jwk/jwkbb"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
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

func (ts *JWTTests) SetupTest() {
	var err error
	ts.TempDir, err = os.MkdirTemp(os.TempDir(), "jwt-test")
	assert.NoError(ts.T(), err)

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(ts.T(), err)
	ecKeyBytes, err := jwkbb.EncodePEM(ecKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), os.WriteFile(ts.TempDir+"/ec", ecKeyBytes, 0600))

	ecPubKeyBytes, err := jwkbb.EncodePEM(&ecKey.PublicKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), os.WriteFile(ts.TempDir+"/ec.pub", ecPubKeyBytes, 0600))

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(ts.T(), err)

	rsaKeyBytes, err := jwkbb.EncodePEM(rsaKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), os.WriteFile(ts.TempDir+"/rsa", rsaKeyBytes, 0600))

	rsaPubKeyBytes, err := jwkbb.EncodePEM(&rsaKey.PublicKey)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), os.WriteFile(ts.TempDir+"/rsa.pub", rsaPubKeyBytes, 0600))
}

func (ts *JWTTests) TearDownTest() {
	_ = os.RemoveAll(ts.TempDir)
}

func (ts *JWTTests) TestGenerateJwtToken() {
	type KeyAlgo struct {
		Algorithm string
		Keyfile   string
		Pubfile   string
	}

	algorithms := []KeyAlgo{
		{Algorithm: "RS256", Keyfile: ts.TempDir + "/rsa", Pubfile: ts.TempDir + "/rsa.pub"},
		{Algorithm: "ES256", Keyfile: ts.TempDir + "/ec", Pubfile: ts.TempDir + "/ec.pub"},
	}

	claims := map[string]any{
		jwt.ExpirationKey: time.Now().UTC().Add(2 * time.Hour),
		jwt.IssuedAtKey:   time.Now().UTC(),
		jwt.IssuerKey:     "http://local.issuer",
		jwt.SubjectKey:    "test@foo.bar",
	}

	for _, test := range algorithms {
		t, expiration, err := generateJwtToken(claims, test.Keyfile, test.Algorithm)
		assert.NoError(ts.T(), err)
		assert.NotNil(ts.T(), t)

		keyData, err := os.ReadFile(test.Pubfile)
		assert.NoError(ts.T(), err)
		key, err := jwk.ParseKey(keyData, jwk.WithX509(true))
		assert.NoError(ts.T(), err)
		assert.NoError(ts.T(), jwk.AssignKeyID(key))
		keySet := jwk.NewSet()
		assert.NoError(ts.T(), keySet.AddKey(key))

		token, err := jwt.Parse([]byte(t), jwt.WithKeySet(keySet, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		assert.NoError(ts.T(), err)
		iss, _ := token.Issuer()
		assert.Equal(ts.T(), "http://local.issuer", iss)
		sub, _ := token.Subject()
		assert.Equal(ts.T(), "test@foo.bar", sub)

		// check that the expiration string is a date
		_, err = time.Parse("2006-01-02 15:04:05", expiration)
		assert.Nil(ts.T(), err, "Couldn't parse expiration date for jwt")
	}
}
