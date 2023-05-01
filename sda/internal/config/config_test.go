package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	helper "sensitive-data-archive/internal/helper"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
}

var certPath, rootDir string

func (suite *ConfigTestSuite) SetupTest() {
	_, b, _, _ := runtime.Caller(0)
	rootDir = path.Join(path.Dir(b), "../../../")
	// pwd, _ = os.Getwd()
	certPath, _ = os.MkdirTemp("", "gocerts")
	helper.MakeCerts(certPath)

	viper.Set("broker.host", "testhost")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "testuser")
	viper.Set("broker.password", "testpassword")
	viper.Set("broker.routingkey", "routingtest")
	viper.Set("broker.exchange", "testexchange")
	viper.Set("broker.vhost", "testvhost")
	viper.Set("aws.url", "testurl")
	viper.Set("aws.accesskey", "testaccess")
	viper.Set("aws.secretkey", "testsecret")
	viper.Set("aws.bucket", "testbucket")
	viper.Set("server.jwtpubkeypath", "testpath")
}

func (suite *ConfigTestSuite) TearDownTest() {
	viper.Reset()
	defer os.RemoveAll(certPath)
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (suite *ConfigTestSuite) TestConfigFile() {
	viper.Set("server.confFile", rootDir+"/.github/integration/sda/config.yaml")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestWrongConfigFile() {
	viper.Set("server.confFile", rootDir+"/.github/integration/rabbitmq/cega.conf")
	config, err := NewConfig()
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/rabbitmq/cega.conf")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestConfigPath() {
	viper.Reset()
	viper.Set("server.confPath", rootDir+"/.github/integration/sda/")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestNoConfig() {
	viper.Reset()
	config, err := NewConfig()
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
}

func (suite *ConfigTestSuite) TestMissingRequiredConfVar() {
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

func (suite *ConfigTestSuite) TestConfigS3Storage() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.S3)
	assert.Equal(suite.T(), "testurl", config.S3.URL)
	assert.Equal(suite.T(), "testaccess", config.S3.AccessKey)
	assert.Equal(suite.T(), "testsecret", config.S3.SecretKey)
	assert.Equal(suite.T(), "testbucket", config.S3.Bucket)
}

func (suite *ConfigTestSuite) TestConfigBroker() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.S3)
	assert.Equal(suite.T(), "/testvhost", config.Broker.Vhost)
	assert.Equal(suite.T(), false, config.Broker.Ssl)

	viper.Set("broker.ssl", true)
	viper.Set("broker.verifyPeer", true)
	_, err = NewConfig()
	assert.Error(suite.T(), err, "Error expected")
	viper.Set("broker.clientCert", "dummy-value")
	viper.Set("broker.clientKey", "dummy-value")
	_, err = NewConfig()
	assert.NoError(suite.T(), err)

	viper.Set("broker.vhost", nil)
	config, err = NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "/", config.Broker.Vhost)
}

func (suite *ConfigTestSuite) TestTLSConfigBroker() {
	viper.Set("broker.serverName", "broker")
	viper.Set("broker.ssl", true)
	viper.Set("broker.cacert", certPath+"/ca.crt")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err := TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsBroker)
	assert.NoError(suite.T(), err)

	viper.Set("broker.verifyPeer", true)
	viper.Set("broker.clientCert", certPath+"/tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsBroker)
	assert.NoError(suite.T(), err)

	viper.Set("broker.clientCert", certPath+"tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.Nil(suite.T(), tlsBroker)
	assert.Error(suite.T(), err)
}

func (suite *ConfigTestSuite) TestTLSConfigProxy() {
	viper.Set("aws.cacert", certPath+"/ca.crt")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsProxy, err := TLSConfigProxy(config)
	assert.NotNil(suite.T(), tlsProxy)
	assert.NoError(suite.T(), err)
}

func (suite *ConfigTestSuite) TestDefaultLogLevel() {
	viper.Set("log.level", "test")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), log.TraceLevel, log.GetLevel())
}
