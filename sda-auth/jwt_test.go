package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type JWTTests struct {
	suite.Suite
	TempDir    string
	ECKeyFile  *os.File
	ECPubFile  *os.File
	RSAKeyFile *os.File
	RSAPubFile *os.File
}

func TestJWTTestSuite(t *testing.T) {
	suite.Run(t, new(JWTTests))
}

func (suite *JWTTests) SetupTest() {
	var err error

	// Create a temporary directory for our config file
	suite.TempDir, err = os.MkdirTemp(os.TempDir(), "sda-auth-test-")
	if err != nil {
		log.Fatal("Couldn't create temporary test directory", err)
	}

	// Create RSA private key file
	suite.RSAKeyFile, err = os.CreateTemp(suite.TempDir, "rsakey-")
	if err != nil {
		log.Fatal("Cannot create temporary rsa key file", err)
	}

	RSAPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Error("Failed to generate RSA key")
	}

	var privateKeyBytes = x509.MarshalPKCS1PrivateKey(RSAPrivateKey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	err = pem.Encode(suite.RSAKeyFile, privateKeyBlock)
	if err != nil {
		log.Error("Error writing RSA private key")
	}

	// Create RSA public key file
	suite.RSAPubFile, err = os.CreateTemp(suite.TempDir, "rsapub-")
	if err != nil {
		log.Fatal("Cannot create temporary rsa pub file", err)
	}

	RSAPublicKey := &RSAPrivateKey.PublicKey
	RSApublicKeyBytes, err := x509.MarshalPKIXPublicKey(RSAPublicKey)
	if err != nil {
		log.Error("Error marshal RSA public key")
	}
	RSApublicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: RSApublicKeyBytes,
	}
	err = pem.Encode(suite.RSAPubFile, RSApublicKeyBlock)
	if err != nil {
		log.Error("Error writing RSA public key")
	}

	// Create EC private key file
	suite.ECKeyFile, err = os.CreateTemp(suite.TempDir, "eckey-")
	if err != nil {
		log.Fatal("Cannot create temporary ec key file", err)
	}

	ECPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Error("Failed to generate EC key")
	}

	privateKeyBytes, err = x509.MarshalECPrivateKey(ECPrivateKey)
	if err != nil {
		log.Error("Failed to marshal EC key")
	}
	privateKeyBlock = &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	err = pem.Encode(suite.ECKeyFile, privateKeyBlock)
	if err != nil {
		log.Error("Error writing EC private key")
	}

	// Create EC public key file
	suite.ECPubFile, err = os.CreateTemp(suite.TempDir, "ecpub-")
	if err != nil {
		log.Fatal("Cannot create temporary ec pub file", err)
	}
	ECPublicKey := &ECPrivateKey.PublicKey
	ECpublicKeyBytes, err := x509.MarshalPKIXPublicKey(ECPublicKey)
	if err != nil {
		log.Error("Error marshal EC public key")
	}
	ECpublicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: ECpublicKeyBytes,
	}
	err = pem.Encode(suite.ECPubFile, ECpublicKeyBlock)
	if err != nil {
		log.Error("Error writing EC public key")
	}
}

func (suite *JWTTests) TearDownTest() {
	os.Remove(suite.RSAKeyFile.Name())
	os.Remove(suite.RSAPubFile.Name())
	os.Remove(suite.ECKeyFile.Name())
	os.Remove(suite.ECPubFile.Name())
	os.Remove(suite.TempDir)
}

func (suite *JWTTests) TestGenerateJwtToken() {

	type KeyAlgo struct {
		Algorithm string
		Keyfile   string
		Pubfile   string
	}

	algorithms := []KeyAlgo{
		{Algorithm: "RS256", Keyfile: suite.RSAKeyFile.Name(), Pubfile: suite.RSAPubFile.Name()},
		{Algorithm: "ES256", Keyfile: suite.ECKeyFile.Name(), Pubfile: suite.ECPubFile.Name()},
	}

	claims := &Claims{
		"test@foo.bar",
		"",
		jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
			Issuer:   "http://local.issuer",
			Subject:  "test@foo.bar",
		},
	}

	for _, test := range algorithms {
		ts, expiration, err := generateJwtToken(claims, test.Keyfile, test.Algorithm)
		assert.NoError(suite.T(), err)
		assert.NotNil(suite.T(), ts)

		t, err := jwt.Parse(ts, func(t *jwt.Token) (interface{}, error) {
			// Validate that the alg is what we expect: RSA or ES
			_, okRSA := t.Method.(*jwt.SigningMethodRSA)
			if okRSA {
				pub, _ := os.ReadFile(test.Pubfile)
				publicKey, _ := jwt.ParseRSAPublicKeyFromPEM(pub)

				return publicKey, nil
			}
			_, okES := t.Method.(*jwt.SigningMethodECDSA)
			if okES {
				pub, _ := os.ReadFile(test.Pubfile)
				publicKey, _ := jwt.ParseECPublicKeyFromPEM(pub)

				return publicKey, nil
			}

			return nil, fmt.Errorf("unexpected signing method")
		})
		assert.NoError(suite.T(), err)
		assert.Equal(suite.T(), test.Algorithm, t.Method.Alg())
		claims := t.Claims.(jwt.MapClaims)
		assert.Equal(suite.T(), "http://local.issuer", claims["iss"])
		assert.Equal(suite.T(), "test@foo.bar", claims["email"])

		// check that the expiration string is a date
		_, err = time.Parse("2006-01-02 15:04:05", expiration)
		assert.Nil(suite.T(), err, "Couldn't parse expiration date for jwt")

	}
}
