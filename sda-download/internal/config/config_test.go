package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var requiredConfVars = []string{
	"db.host", "db.user", "db.password", "db.database", "oidc.configuration.url", "grpc.host",
}

type TestSuite struct {
	suite.Suite
}

func (suite *TestSuite) SetupTest() {
	viper.Set("db.host", "test")
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")
	viper.Set("c4gh.filepath", "test")
	viper.Set("c4gh.passphrase", "test")
	viper.Set("oidc.configuration.url", "test")
}

func (suite *TestSuite) TearDownTest() {
	viper.Reset()
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestConfigFile() {
	viper.Set("configFile", "test")
	config, err := NewConfig()
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), "test", viper.ConfigFileUsed())
}

func (suite *TestSuite) TestMissingRequiredConfVar() {
	for _, requiredConfVar := range requiredConfVars {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig()
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestAppConfig() {

	// Test fail on missing middleware
	viper.Set("app.host", "test")
	viper.Set("app.port", 1234)
	viper.Set("app.servercert", "test")
	viper.Set("app.serverkey", "test")
	viper.Set("log.logLevel", "debug")
	viper.Set("db.sslmode", "disable")

	viper.Set("app.middleware", "noexist")

	c := &Map{}
	err := c.appConfig()
	assert.Error(suite.T(), err, "Error expected")
	viper.Reset()

	viper.Set("app.host", "test")
	viper.Set("app.port", 1234)
	viper.Set("app.serveUnencryptedData", false)
	viper.Set("app.servercert", "test")
	viper.Set("app.serverkey", "test")
	viper.Set("log.logLevel", "debug")
	viper.Set("db.sslmode", "disable")

	c = &Map{}
	err = c.appConfig()
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test", c.App.Host)
	assert.Equal(suite.T(), 1234, c.App.Port)
	assert.Equal(suite.T(), "test", c.App.ServerCert)
	assert.Equal(suite.T(), "test", c.App.ServerKey)
	assert.NotNil(suite.T(), c.App.Crypt4GHPrivateKey)
	assert.NotNil(suite.T(), c.App.Crypt4GHPublicKeyB64)
	assert.Equal(suite.T(), false, c.App.ServeUnencryptedData)

	// Check the key that was generated
	publicKey, err := base64.StdEncoding.DecodeString(c.App.Crypt4GHPublicKeyB64)
	assert.Nilf(suite.T(), err, "Incorrect public c4gh key generated (error in base64 encoding)")
	_, err = keys.ReadPublicKey(bytes.NewReader(publicKey))
	assert.Nilf(suite.T(), err, "Incorrect public c4gh key generated (bad key)")
}

func (suite *TestSuite) TestArchiveConfig() {
	viper.Set("archive.type", POSIX)
	viper.Set("archive.location", "/test")

	c := &Map{}
	c.configArchive()
	assert.Equal(suite.T(), "/test", c.Archive.Posix.Location)

}

func (suite *TestSuite) TestSessionConfig() {

	viper.Set("session.expiration", 3600)
	viper.Set("session.domain", "test")
	viper.Set("session.secure", false)
	viper.Set("session.httponly", false)

	viper.Set("db.sslmode", "disable")

	c := &Map{}
	c.sessionConfig()
	assert.Equal(suite.T(), time.Duration(3600*time.Second), c.Session.Expiration)
	assert.Equal(suite.T(), "test", c.Session.Domain)
	assert.Equal(suite.T(), false, c.Session.Secure)
	assert.Equal(suite.T(), false, c.Session.HTTPOnly)

}

func (suite *TestSuite) TestDatabaseConfig() {

	// Test error on missing SSL vars
	viper.Set("db.sslmode", "verify-full")
	c := &Map{}
	err := c.configDatabase()
	assert.Error(suite.T(), err, "Error expected")

	// Test no error on SSL disabled
	viper.Set("db.sslmode", "disable")
	c = &Map{}
	err = c.configDatabase()
	assert.NoError(suite.T(), err)

	// Test pass on SSL vars set
	viper.Set("db.host", "test")
	viper.Set("db.port", 1234)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")
	viper.Set("db.cacert", "test")
	viper.Set("db.clientcert", "test")
	viper.Set("db.clientkey", "test")
	viper.Set("db.sslmode", "verify-full")

	c = &Map{}
	err = c.configDatabase()
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test", c.DB.Host)
	assert.Equal(suite.T(), 1234, c.DB.Port)
	assert.Equal(suite.T(), "test", c.DB.User)
	assert.Equal(suite.T(), "test", c.DB.Password)
	assert.Equal(suite.T(), "test", c.DB.Database)
	assert.Equal(suite.T(), "test", c.DB.CACert)
	assert.Equal(suite.T(), "test", c.DB.ClientCert)
	assert.Equal(suite.T(), "test", c.DB.ClientKey)

}

func (suite *TestSuite) TestOIDC() {

	// Test wrong file
	viper.Set("oidc.trusted.iss", "../../iss.json")
	c := &Map{}
	err := c.configureOIDC()
	assert.Error(suite.T(), err, "Error expected")

	viper.Set("oidc.trusted.iss", "../../dev_utils/iss.json")
	c = &Map{}
	err = c.configureOIDC()
	assert.NoError(suite.T(), err)

	// Test pass OIDC config
	viper.Set("oidc.trusted.iss", "../../dev_utils/iss.json")
	viper.Set("oidc.configuration.url", "test")
	viper.Set("oidc.cacert", "test")

	trustedList := []TrustedISS([]TrustedISS{{ISS: "https://demo.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}, {ISS: "https://demo1.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}})

	whitelist := jwk.NewMapWhitelist().Add("https://mockauth:8000/idp/profile/oidc/keyset")
	c = &Map{}
	err = c.configureOIDC()
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test", c.OIDC.ConfigurationURL)
	assert.Equal(suite.T(), "test", c.OIDC.CACert)
	assert.Equal(suite.T(), trustedList, c.OIDC.TrustedList)
	assert.Equal(suite.T(), whitelist, c.OIDC.Whitelist)

}

func (suite *TestSuite) TestConfigReencrypt() {
	tempDir := suite.T().TempDir()
	c := &Map{}
	viper.Set("grpc.host", "localhost")
	assert.NoError(suite.T(), c.configReencrypt())
	assert.Equal(suite.T(), 50051, c.Reencrypt.Port)

	// fail if set file doesn't exists
	viper.Set("grpc.clientcert", "/tmp/abracadabra")
	assert.ErrorContains(suite.T(), c.configReencrypt(), "no such file or directory")

	// any existing flle will make it pass
	viper.Set("grpc.clientcert", "config_test.go")
	assert.NoError(suite.T(), c.configReencrypt())

	// it will fail if certificate is set to a folder
	viper.Set("grpc.clientcert", tempDir)
	assert.ErrorContains(suite.T(), c.configReencrypt(), "is a folder")

}
