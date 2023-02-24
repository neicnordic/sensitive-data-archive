package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"
)

// These are not complete tests of all functions in elixir. New tests should
// be added as the code is updated.

type ConfigTests struct {
	suite.Suite
	TempDir           string
	ConfigFile        *os.File
	ElixirConfig      ElixirConfig
	CegaConfig        CegaConfig
	ServerConfig      ServerConfig
	S3Inbox           string
	JwtIssuer         string
	JwtPrivateKey     string
	JwtPrivateKeyFile *os.File
	JwtSignatureAlg   string
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTests))
}

func (suite *ConfigTests) SetupTest() {

	var err error

	// Create a temporary directory for our config file
	suite.TempDir, err = os.MkdirTemp("", "sda-auth-test-")
	if err != nil {
		log.Fatal("Couldn't create temporary test directory", err)
	}
	suite.ConfigFile, err = os.Create(filepath.Join(suite.TempDir, "config.yaml"))
	if err != nil {
		log.Fatal("Cannot create temporary config file", err)
	}

	// Create temporary dummy keys
	suite.JwtPrivateKeyFile, err = os.Create(filepath.Join(suite.TempDir, "jwt-dummy-sec.c4gh"))
	if err != nil {
		log.Fatal("Cannot create temporary private key file", err)
	}
	_, err = os.Create(filepath.Join(suite.TempDir, "jwt-dummy-sec.c4gh_env"))
	if err != nil {
		log.Fatal("Cannot create temporary private key file", err)
	}

	// config values to write to the config file
	suite.ElixirConfig = ElixirConfig{
		ID:            "elixirTestID",
		Provider:      "elixirTestIssuer",
		RedirectURL:   "http://elixir/login",
		RevocationURL: "http://elixir/revoke",
		Secret:        "elixirTestSecret",
	}

	suite.CegaConfig = CegaConfig{
		AuthURL: "http://cega/auth",
		ID:      "cegaID",
		Secret:  "cegaSecret",
	}

	suite.ServerConfig = ServerConfig{
		Cert: "serverCert.pem",
		Key:  "serverKey.pem",
	}

	suite.S3Inbox = "s3://testInbox"
	suite.JwtIssuer = "JwtIssuer"
	suite.JwtPrivateKey = suite.JwtPrivateKeyFile.Name()
	suite.JwtSignatureAlg = "RS256"

	// Write config to temp config file
	configYaml, err := yaml.Marshal(Config{
		Elixir:          suite.ElixirConfig,
		Cega:            suite.CegaConfig,
		Server:          suite.ServerConfig,
		S3Inbox:         suite.S3Inbox,
		JwtIssuer:       suite.JwtIssuer,
		JwtPrivateKey:   suite.JwtPrivateKey,
		JwtSignatureAlg: suite.JwtSignatureAlg,
	})
	if err != nil {
		log.Errorf("Error marshalling config yaml: %v", err)
	}
	_, err = suite.ConfigFile.Write(configYaml)
	if err != nil {
		log.Errorf("Error writing config file: %v", err)
	}

}

func (suite *ConfigTests) TearDownTest() {
	os.RemoveAll(suite.TempDir)
}

// Both readConfig and parseConfig is called when using NewConfig, so they are
// both tested in this single test.
func (suite *ConfigTests) TestConfig() {
	// change dir so that we read the right config
	err := os.Chdir(suite.TempDir)
	if err != nil {
		log.Errorf("Couldn't access temp directory: %v", err)
	}

	config, err := NewConfig()
	assert.NoError(suite.T(), err)

	// Check elixir values
	assert.Equal(suite.T(), suite.ElixirConfig.ID, config.Elixir.ID, "Elixir ID misread from config file")
	assert.Equal(suite.T(), suite.ElixirConfig.Provider, config.Elixir.Provider, "Elixir Issuer misread from config file")
	assert.Equal(suite.T(), suite.ElixirConfig.RedirectURL, config.Elixir.RedirectURL, "Elixir RedirectURL misread from config file")
	assert.Equal(suite.T(), suite.ElixirConfig.Secret, config.Elixir.Secret, "Elixir Secret misread from config file")

	// Check CEGA values
	assert.Equal(suite.T(), suite.CegaConfig.ID, config.Cega.ID, "CEGA ID misread from config file")
	assert.Equal(suite.T(), suite.CegaConfig.AuthURL, config.Cega.AuthURL, "CEGA AuthURL misread from config file")
	assert.Equal(suite.T(), suite.CegaConfig.Secret, config.Cega.Secret, "CEGA Secret misread from config file")

	// Check ServerConfig values
	assert.Equal(suite.T(), suite.ServerConfig.Cert, config.Server.Cert, "ServerConfig Cert misread from config file")
	assert.Equal(suite.T(), suite.ServerConfig.Key, config.Server.Key, "ServerConfig Key misread from config file")

	// Check S3Inbox value
	assert.Equal(suite.T(), suite.S3Inbox, config.S3Inbox, "S3Inbox misread from config file")

	// Check JWT values
	assert.Equal(suite.T(), suite.JwtIssuer, config.JwtIssuer, "JwtIssuer misread from config file")
	assert.Equal(suite.T(), suite.JwtPrivateKey, config.JwtPrivateKey, "JwtPrivateKey misread from config file")
	assert.Equal(suite.T(), suite.JwtSignatureAlg, config.JwtSignatureAlg, "JwtSignatureAlg misread from config file")

	// sanitycheck without config file or ENVs
	// this should fail
	os.Remove(suite.ConfigFile.Name())
	viper.Reset()
	_, e := NewConfig()
	assert.Error(suite.T(), e)

	// Set all values as environment variables
	os.Setenv("ELIXIR_ID", fmt.Sprintf("env_%v", suite.ElixirConfig.ID))
	os.Setenv("ELIXIR_PROVIDER", fmt.Sprintf("env_%v", suite.ElixirConfig.Provider))
	os.Setenv("ELIXIR_REDIRECTURL", fmt.Sprintf("env_%v", suite.ElixirConfig.RedirectURL))
	os.Setenv("ELIXIR_SECRET", fmt.Sprintf("env_%v", suite.ElixirConfig.Secret))

	os.Setenv("CEGA_ID", fmt.Sprintf("env_%v", suite.CegaConfig.ID))
	os.Setenv("CEGA_AUTHURL", fmt.Sprintf("env_%v", suite.CegaConfig.AuthURL))
	os.Setenv("CEGA_SECRET", fmt.Sprintf("env_%v", suite.CegaConfig.Secret))

	os.Setenv("SERVER_CERT", fmt.Sprintf("env_%v", suite.ServerConfig.Cert))
	os.Setenv("SERVER_KEY", fmt.Sprintf("env_%v", suite.ServerConfig.Key))

	os.Setenv("S3INBOX", fmt.Sprintf("env_%v", suite.S3Inbox))

	os.Setenv("JWTISSUER", fmt.Sprintf("env_%v", suite.JwtIssuer))
	os.Setenv("JWTPRIVATEKEY", fmt.Sprintf("%v_env", suite.JwtPrivateKey))
	os.Setenv("JWTSIGNATUREALG", fmt.Sprintf("env_%v", suite.JwtSignatureAlg))

	// re-read the config
	config, err = NewConfig()
	assert.NoError(suite.T(), err)

	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ElixirConfig.ID), config.Elixir.ID, "Elixir ID misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ElixirConfig.Provider), config.Elixir.Provider, "Elixir Issuer misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ElixirConfig.RedirectURL), config.Elixir.RedirectURL, "Elixir RedirectURL misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ElixirConfig.Secret), config.Elixir.Secret, "Elixir Secret misread from environment variable")

	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.CegaConfig.ID), config.Cega.ID, "CEGA ID misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.CegaConfig.AuthURL), config.Cega.AuthURL, "CEGA AuthURL misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.CegaConfig.Secret), config.Cega.Secret, "CEGA Secret misread from environment variable")

	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ServerConfig.Cert), config.Server.Cert, "ServerConfig Cert misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.ServerConfig.Key), config.Server.Key, "ServerConfig Key misread from environment variable")

	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.S3Inbox), config.S3Inbox, "S3Inbox misread from environment variable")

	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.JwtIssuer), config.JwtIssuer, "JwtIssuer misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("%v_env", suite.JwtPrivateKey), config.JwtPrivateKey, "JwtPrivateKey misread from environment variable")
	assert.Equal(suite.T(), fmt.Sprintf("env_%v", suite.JwtSignatureAlg), config.JwtSignatureAlg, "JwtSignatureAlg misread from environment variable")

	// Check missing private key
	os.Setenv("JWTPRIVATEKEY", "nonexistent-key-file")

	// re-read the config
	_, err = NewConfig()
	assert.ErrorContains(suite.T(), err, "Error when reading from private key file")
}
