package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
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

func (ts *TestSuite) SetupTest() {
	viper.Set("db.host", "test")
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")
	viper.Set("c4gh.filepath", "test")
	viper.Set("c4gh.passphrase", "test")
	viper.Set("oidc.configuration.url", "test")
}

func (ts *TestSuite) TearDownTest() {
	viper.Reset()
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) TestConfigFile() {
	viper.Set("configFile", "test")
	config, err := NewConfig()
	assert.Nil(ts.T(), config)
	assert.Error(ts.T(), err)
	assert.Equal(ts.T(), "test", viper.ConfigFileUsed())
}

func (ts *TestSuite) TestMissingRequiredConfVar() {
	for _, requiredConfVar := range requiredConfVars {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig()
		assert.Nil(ts.T(), config)
		if assert.Error(ts.T(), err) {
			assert.Equal(ts.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (ts *TestSuite) TestAppConfig() {
	// Generate a crypth4gh private key file
	_, privateKey, err := keys.GenerateKeyPair()
	assert.NoError(ts.T(), err)
	tempDir := ts.T().TempDir()
	defer os.RemoveAll(tempDir)
	privateKeyFile, err := os.Create(fmt.Sprintf("%s/c4fg.key", tempDir))
	assert.NoError(ts.T(), err)
	err = keys.WriteCrypt4GHX25519PrivateKey(privateKeyFile, privateKey, []byte("password"))
	assert.NoError(ts.T(), err)

	// Test fail on missing middleware
	viper.Set("app.host", "test")
	viper.Set("app.port", 1234)
	viper.Set("app.servercert", "test")
	viper.Set("app.serverkey", "test")
	viper.Set("log.logLevel", "debug")
	viper.Set("db.sslmode", "disable")

	viper.Set("app.middleware", "noexist")
	viper.Set("app.expectedcliversion", "v0.2.0")

	c := &Map{}
	err = c.appConfig()
	assert.Error(ts.T(), err, "Error expected")
	viper.Reset()

	// Test fail on invalid expected client version
	viper.Set("app.expectedcliversion", "not-a-semver")
	c = &Map{}
	err = c.appConfig()
	assert.Error(ts.T(), err, "Error expected for invalid semver string")
	assert.Contains(ts.T(), err.Error(), "'not-a-semver' is not a valid semantic version")
	viper.Reset()

	viper.Set("app.host", "test")
	viper.Set("app.port", 1234)
	viper.Set("app.servercert", "test")
	viper.Set("app.serverkey", "test")
	viper.Set("app.expectedcliversion", "v0.2.0")
	viper.Set("log.logLevel", "debug")
	viper.Set("db.sslmode", "disable")
	viper.Set("c4gh.transientKeyPath", privateKeyFile.Name())
	viper.Set("c4gh.transientPassphrase", "password")

	c = &Map{}
	err = c.appConfig()
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "test", c.App.Host)
	assert.Equal(ts.T(), 1234, c.App.Port)
	assert.Equal(ts.T(), "test", c.App.ServerCert)
	assert.Equal(ts.T(), "test", c.App.ServerKey)
	assert.NotEmpty(ts.T(), c.C4GH.PrivateKey)
	assert.NotEmpty(ts.T(), c.C4GH.PublicKeyB64)

	// Assert ExpectedCliVersion
	expectedVersion, _ := semver.NewVersion("v0.2.0")
	assert.NotNil(ts.T(), c.App.ExpectedCliVersion, "ExpectedCliVersion should be parsed and not nil")
	assert.True(ts.T(), expectedVersion.Equal(c.App.ExpectedCliVersion), "Parsed version does not match expected version")

	// Check the private key that was loaded by checking the derived public key
	publicKey, err := base64.StdEncoding.DecodeString(c.C4GH.PublicKeyB64)
	assert.Nilf(ts.T(), err, "Incorrect public c4gh key generated (error in base64 encoding)")
	_, err = keys.ReadPublicKey(bytes.NewReader(publicKey))
	assert.Nilf(ts.T(), err, "Incorrect public c4gh key generated (bad key)")

	// Check false c4gh key
	viper.Set("c4gh.transientKeyPath", "some/nonexistent.key")
	err = c.appConfig()
	assert.ErrorContains(ts.T(), err, "no such file or directory")

	// Check false c4gh key
	viper.Set("c4gh.transientKeyPath", privateKeyFile.Name())
	viper.Set("c4gh.transientPassphrase", "blablabla")
	err = c.appConfig()
	assert.ErrorContains(ts.T(), err, "chacha20poly1305: message authentication failed")
}

func (ts *TestSuite) TestArchiveConfig() {
	viper.Set("archive.type", POSIX)
	viper.Set("archive.location", "/test")

	c := &Map{}
	c.configArchive()
	assert.Equal(ts.T(), "/test", c.Archive.Posix.Location)
}

func (ts *TestSuite) TestSessionConfig() {
	viper.Set("session.expiration", 3600)
	viper.Set("session.domain", "test")
	viper.Set("session.secure", false)
	viper.Set("session.httponly", false)

	viper.Set("db.sslmode", "disable")

	c := &Map{}
	c.sessionConfig()
	assert.Equal(ts.T(), time.Duration(3600*time.Second), c.Session.Expiration)
	assert.Equal(ts.T(), "test", c.Session.Domain)
	assert.Equal(ts.T(), false, c.Session.Secure)
	assert.Equal(ts.T(), false, c.Session.HTTPOnly)
}

func (ts *TestSuite) TestDatabaseConfig() {
	// Test error on missing SSL vars
	viper.Set("db.sslmode", "verify-full")
	c := &Map{}
	err := c.configDatabase()
	assert.Error(ts.T(), err, "Error expected")

	// Test no error on SSL disabled
	viper.Set("db.sslmode", "disable")
	c = &Map{}
	err = c.configDatabase()
	assert.NoError(ts.T(), err)

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
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "test", c.DB.Host)
	assert.Equal(ts.T(), 1234, c.DB.Port)
	assert.Equal(ts.T(), "test", c.DB.User)
	assert.Equal(ts.T(), "test", c.DB.Password)
	assert.Equal(ts.T(), "test", c.DB.Database)
	assert.Equal(ts.T(), "test", c.DB.CACert)
	assert.Equal(ts.T(), "test", c.DB.ClientCert)
	assert.Equal(ts.T(), "test", c.DB.ClientKey)
}

func (ts *TestSuite) TestOIDC() {
	// Test wrong file
	viper.Set("oidc.trusted.iss", "../../iss.json")
	c := &Map{}
	err := c.configureOIDC()
	assert.Error(ts.T(), err, "Error expected")

	viper.Set("oidc.trusted.iss", "../../dev_utils/iss.json")
	c = &Map{}
	err = c.configureOIDC()
	assert.NoError(ts.T(), err)

	// Test pass OIDC config
	viper.Set("oidc.trusted.iss", "../../dev_utils/iss.json")
	viper.Set("oidc.configuration.url", "test")
	viper.Set("oidc.cacert", "test")

	trustedList := []TrustedISS([]TrustedISS{{ISS: "https://demo.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}, {ISS: "https://demo1.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}})

	whitelist := jwk.NewMapWhitelist().Add("https://mockauth:8000/idp/profile/oidc/keyset")
	c = &Map{}
	err = c.configureOIDC()
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "test", c.OIDC.ConfigurationURL)
	assert.Equal(ts.T(), "test", c.OIDC.CACert)
	assert.Equal(ts.T(), trustedList, c.OIDC.TrustedList)
	assert.Equal(ts.T(), whitelist, c.OIDC.Whitelist)
}

func (ts *TestSuite) TestConfigReencrypt() {
	tempDir := ts.T().TempDir()
	c := &Map{}
	viper.Set("grpc.host", "localhost")
	assert.NoError(ts.T(), c.configReencrypt())
	assert.Equal(ts.T(), 50051, c.Reencrypt.Port)

	// fail if set file doesn't exists
	viper.Set("grpc.clientcert", "/tmp/abracadabra")
	assert.ErrorContains(ts.T(), c.configReencrypt(), "no such file or directory")

	// any existing flle will make it pass
	viper.Set("grpc.clientcert", "config_test.go")
	assert.NoError(ts.T(), c.configReencrypt())

	// it will fail if certificate is set to a folder
	viper.Set("grpc.clientcert", tempDir)
	assert.ErrorContains(ts.T(), c.configReencrypt(), "is a folder")
}
