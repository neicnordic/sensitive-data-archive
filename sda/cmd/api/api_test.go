package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var (
	CorrectToken = map[string]interface{}{
		"sub":   "requester@demo.org",
		"azp":   "azp",
		"scope": "openid ga4gh_passport_v1",
		"iss":   "http://example.demo",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "6ad7aa42-3e9c-4833-bd16-765cb80c2102",
	}

	NoIssuer = map[string]interface{}{
		"sub":   "requester@demo.org",
		"azp":   "azp",
		"scope": "openid ga4gh_passport_v1",
		"exp":   time.Now().Add(time.Hour * 2).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "6ad7aa42-3e9c-4833-bd16-765cb80c2102",
	}
)

type TestSuite struct {
	suite.Suite
	PrivateKey *rsa.PrivateKey
	Path       string
	KeyName    string
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

// Remove the created keys after all tests are run
func (suite *TestSuite) TearDownTest() {
	os.Remove(suite.Path + suite.KeyName + ".pem")
	os.Remove(suite.Path + suite.KeyName + ".pub")
}

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")

	viper.Set("db.host", "test")
	viper.Set("db.port", 123)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")

	conf := config.Config{}
	conf.API.Host = "localhost"
	conf.API.Port = 8080
	server := setup(&conf)

	assert.Equal(suite.T(), "localhost:8080", server.Addr)

	suite.Path = "/tmp/keys/"
	suite.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	suite.CreateKeys(suite.Path, suite.KeyName)

}

// CreateKeys creates an RSA key pair for testing
func (suite *TestSuite) CreateKeys(path string, keyName string) {
	err := CreateFolder(path)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	err = CreateRSAkeys(path, keyName)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	suite.PrivateKey, err = ParsePrivateRSAKey(path, keyName+".pem")
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}

// CreateFolder where the keys will be stored
func CreateFolder(path string) error {
	err := os.MkdirAll(path, 0750)
	if err != nil {
		return err
	}

	return nil
}

// ParsePrivateRSAKey reads and parses the RSA private key
func ParsePrivateRSAKey(path, keyName string) (*rsa.PrivateKey, error) {
	keyPath := path + keyName
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(prKey)
	prKeyParsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return prKeyParsed, nil
}

// CreateRSAkeys creates the RSA key pair
func CreateRSAkeys(keyPath string, keyName string) error {
	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	publickey := &privatekey.PublicKey

	// dump private key to file
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privatekey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privatePem, err := os.Create(keyPath + keyName + ".pem")
	if err != nil {
		return err
	}
	err = pem.Encode(privatePem, privateKeyBlock)
	if err != nil {
		return err
	}

	// dump public key to file
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publickey)
	if err != nil {
		return err
	}
	publicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}
	publicPem, err := os.Create(keyPath + keyName + ".pub")
	if err != nil {
		return err
	}
	err = pem.Encode(publicPem, publicKeyBlock)
	if err != nil {
		return err
	}

	return nil
}

// CreateRSAToken creates an RSA token
func CreateRSAToken(privateKey *rsa.PrivateKey, headerAlg string, tokenClaims map[string]interface{}) (string, error) {
	var tok jwt.Token
	tok, err := jwt.NewBuilder().Issuer(fmt.Sprintf("%v", tokenClaims["iss"])).Build()
	if err != nil {
		log.Error(err)
	}

	for key, element := range tokenClaims {
		err = tok.Set(key, element)
		if err != nil {
			log.Error(err)
		}
	}

	serialized, err := jwt.Sign(tok, jwt.WithKey(jwa.SignatureAlgorithm(headerAlg), privateKey))
	if err != nil {
		return "no-token", err
	}

	return string(serialized), nil
}

func TestDatabasePingCheck(t *testing.T) {
	database := database.SQLdb{}
	assert.Error(t, checkDB(&database, 1*time.Second), "nil DB should fail")

	database.DB, _, err = sqlmock.New()
	assert.NoError(t, err)
	assert.NoError(t, checkDB(&database, 1*time.Second), "ping should succeed")
}

func TestGetToken(t *testing.T) {
	authHeader := "Bearer sometoken"
	_, err := getToken(authHeader)
	assert.NoError(t, err)

	authHeader = "Bearer "
	_, err = getToken(authHeader)
	log.Print(err)
	assert.EqualError(t, err, "token string is missing from authorization header")

	authHeader = "Beare"
	_, err = getToken(authHeader)
	assert.EqualError(t, err, "authorization scheme must be bearer")

	authHeader = ""
	_, err = getToken(authHeader)
	assert.EqualError(t, err, "access token must be provided")
}

func (suite *TestSuite) TestGetUserFromToken() {
	c := &config.Config{}
	APIConf := config.APIConf{}
	APIConf.JwtPubKeyPath = "/tmp/keys"
	c.API = APIConf

	Conf = c
	Conf.API.JtwKeys = make(map[string][]byte)

	err := config.GetJwtKey(Conf.API.JwtPubKeyPath, Conf.API.JtwKeys)
	if err != nil {
		log.Fatalf("error in GetJwtKey: %v", err)
	}

	url := "localhost:8080/files"
	method := "GET"
	r, err := http.NewRequest(method, url, nil)
	if err != nil {
		log.Fatalf("error: %v", err)

		return
	}

	// Functional token
	token, err := CreateRSAToken(suite.PrivateKey, "RS256", CorrectToken)
	if err != nil {
		log.Fatalf("error in createToken: %v", err)
	}
	r.Header.Add("Authorization", "Bearer "+token)

	user, err := getUserFromToken(r)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "requester@demo.org", user)

	// Token without authorization header
	r.Header.Del("Authorization")

	user, err = getUserFromToken(r)
	assert.EqualError(suite.T(), err, "could not get token from header: access token must be provided")
	assert.Equal(suite.T(), "", user)

	// Token without issuer
	token, err = CreateRSAToken(suite.PrivateKey, "RS256", NoIssuer)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	r.Header.Add("Authorization", "Bearer "+token)

	user, err = getUserFromToken(r)
	assert.EqualError(suite.T(), err, "failed to get issuer from token (<nil>)")
	assert.Equal(suite.T(), "", user)

}
