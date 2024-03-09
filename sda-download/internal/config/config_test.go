package config

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var requiredConfVars = []string{
	"db.host", "db.user", "db.password", "db.database", "c4gh.filepath", "c4gh.passphrase", "oidc.configuration.url",
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

	// Test fail on key read error
	viper.Set("app.host", "test")
	viper.Set("app.port", 1234)
	viper.Set("app.servercert", "test")
	viper.Set("app.serverkey", "test")
	viper.Set("log.logLevel", "debug")

	viper.Set("db.sslmode", "disable")

	c := &Map{}
	err := c.appConfig()
	assert.Error(suite.T(), err, "Error expected")
	assert.Nil(suite.T(), c.App.Crypt4GHKey)

	// Generate a Crypt4GH private key, so that ConfigMap.appConfig() doesn't fail
	generateKeyForTest(suite)

	c = &Map{}
	err = c.appConfig()
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "test", c.App.Host)
	assert.Equal(suite.T(), 1234, c.App.Port)
	assert.Equal(suite.T(), "test", c.App.ServerCert)
	assert.Equal(suite.T(), "test", c.App.ServerKey)
}

func (suite *TestSuite) TestArchiveConfig() {
	viper.Set("archive.type", POSIX)
	viper.Set("archive.location", "/test")

	c := &Map{}
	c.configArchive()
	assert.Equal(suite.T(), "/test", c.Archive.Posix.Location)

}

func (suite *TestSuite) TestArchiveS3Config() {
	for _, archiveType := range []string{S3, S3seekable} {
		viper.Set("archive.type", archiveType)
		viper.Set("archive.url", "localhost")
		viper.Set("archive.accesskey", "access")
		viper.Set("archive.secretkey", "secret")
		viper.Set("archive.bucket", "bucket")
		viper.Set("archive.port", "9090")
		viper.Set("archive.chunksize", "10")
		viper.Set("archive.region", "us-west-1")
		viper.Set("archive.cacert", "filename")

		c := &Map{}
		c.configArchive()
		assert.Equal(suite.T(), "localhost", c.Archive.S3.URL)
		assert.Equal(suite.T(), "access", c.Archive.S3.AccessKey)
		assert.Equal(suite.T(), "secret", c.Archive.S3.SecretKey)
		assert.Equal(suite.T(), "bucket", c.Archive.S3.Bucket)
		assert.Equal(suite.T(), "us-west-1", c.Archive.S3.Region)
		assert.Equal(suite.T(), "filename", c.Archive.S3.Cacert)
		assert.Equal(suite.T(), 10*1024*1024, c.Archive.S3.Chunksize)
		assert.Equal(suite.T(), 9090, c.Archive.S3.Port)

	}
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
	generateKeyForTest(suite)
	viper.Set("grpc.clientcert", viper.Get("c4gh.filepath"))
	assert.NoError(suite.T(), c.configReencrypt())

	// it will fail if certificate is set to a folder
	generateKeyForTest(suite)
	viper.Set("grpc.clientcert", tempDir)
	assert.ErrorContains(suite.T(), c.configReencrypt(), "is a folder")

}

func generateKeyForTest(suite *TestSuite) {
	// Generate a key, so that ConfigMap.appConfig() doesn't fail
	_, privateKey, err := keys.GenerateKeyPair()
	assert.NoError(suite.T(), err)
	tempDir := suite.T().TempDir()
	privateKeyFile, err := os.Create(fmt.Sprintf("%s/c4fg.key", tempDir))
	assert.NoError(suite.T(), err)
	err = keys.WriteCrypt4GHX25519PrivateKey(privateKeyFile, privateKey, []byte("password"))
	assert.NoError(suite.T(), err)
	viper.Set("c4gh.filepath", fmt.Sprintf("%s/c4fg.key", tempDir))
	viper.Set("c4gh.passphrase", "password")
}
