package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	helper "sensitive-data-archive/internal/helper"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

var certPath string

func (suite *TestSuite) SetupTest() {
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

func (suite *TestSuite) TearDownTest() {
	viper.Reset()
	defer os.RemoveAll(certPath)
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestConfigFile() {
	viper.Set("server.confFile", "dev_utils/config.yaml")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "dev_utils/config.yaml", viper.ConfigFileUsed())
}

func (suite *TestSuite) TestWrongConfigFile() {
	viper.Set("server.confFile", "dev_utils/rabbitmq.conf")
	config, err := NewConfig()
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), "dev_utils/rabbitmq.conf", viper.ConfigFileUsed())
}

func (suite *TestSuite) TestConfigPath() {
	viper.Set("server.confPath", "./dev_utils")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	absPath, _ := filepath.Abs("dev_utils/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *TestSuite) TestNoConfig() {
	viper.Reset()
	config, err := NewConfig()
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
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

func (suite *TestSuite) TestConfigS3Storage() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.S3)
	assert.Equal(suite.T(), "testurl", config.S3.url)
	assert.Equal(suite.T(), "testaccess", config.S3.accessKey)
	assert.Equal(suite.T(), "testsecret", config.S3.secretKey)
	assert.Equal(suite.T(), "testbucket", config.S3.bucket)
}

func (suite *TestSuite) TestConfigBroker() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.S3)
	assert.Equal(suite.T(), "/testvhost", config.Broker.vhost)
	assert.Equal(suite.T(), false, config.Broker.ssl)

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
	assert.Equal(suite.T(), "/", config.Broker.vhost)
}

func (suite *TestSuite) TestTLSConfigBroker() {
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

func (suite *TestSuite) TestTLSConfigProxy() {
	viper.Set("aws.cacert", certPath+"/ca.crt")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsProxy, err := TLSConfigProxy(config)
	assert.NotNil(suite.T(), tlsProxy)
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestDefaultLogLevel() {
	viper.Set("log.level", "test")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), log.TraceLevel, log.GetLevel())
}
