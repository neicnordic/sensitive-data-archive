package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	helper "github.com/neicnordic/sensitive-data-archive/internal/helper"

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
	viper.Set("broker.queue", "testqueue")
	viper.Set("db.host", "test")
	viper.Set("db.port", 123)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")
	viper.Set("inbox.url", "testurl")
	viper.Set("inbox.accesskey", "testaccess")
	viper.Set("inbox.secretkey", "testsecret")
	viper.Set("inbox.bucket", "testbucket")
	viper.Set("server.jwtpubkeypath", "testpath")
	viper.Set("log.level", "debug")
}

func (suite *ConfigTestSuite) TearDownTest() {
	viper.Reset()
	defer os.RemoveAll(certPath)
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (suite *ConfigTestSuite) TestNonExistingApplication() {
	expectedError := errors.New("application 'test' doesn't exist")
	config, err := NewConfig("test")
	assert.Nil(suite.T(), config)
	if assert.Error(suite.T(), err) {
		assert.Equal(suite.T(), expectedError, err)
	}
}

func (suite *ConfigTestSuite) TestConfigFile() {
	viper.Set("configFile", rootDir+"/.github/integration/sda/config.yaml")
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestWrongConfigFile() {
	viper.Set("configFile", rootDir+"/.github/integration/rabbitmq/cega.conf")
	config, err := NewConfig("s3inbox")
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/rabbitmq/cega.conf")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestConfigPath() {
	viper.Reset()
	viper.Set("configPath", rootDir+"/.github/integration/sda/")
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *ConfigTestSuite) TestNoConfig() {
	viper.Reset()
	config, err := NewConfig("s3inbox")
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
}

func (suite *ConfigTestSuite) TestMissingRequiredConfVar() {
	for _, requiredConfVar := range requiredConfVars {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("s3inbox")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *ConfigTestSuite) TestConfigS3Storage() {
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Inbox.S3)
	assert.Equal(suite.T(), "testurl", config.Inbox.S3.URL)
	assert.Equal(suite.T(), "testaccess", config.Inbox.S3.AccessKey)
	assert.Equal(suite.T(), "testsecret", config.Inbox.S3.SecretKey)
	assert.Equal(suite.T(), "testbucket", config.Inbox.S3.Bucket)
}

func (suite *ConfigTestSuite) TestConfigBroker() {
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Inbox.S3)
	assert.Equal(suite.T(), "/testvhost", config.Broker.Vhost)
	assert.Equal(suite.T(), false, config.Broker.Ssl)

	viper.Set("broker.ssl", true)
	viper.Set("broker.verifyPeer", true)
	_, err = NewConfig("s3inbox")
	assert.Error(suite.T(), err, "Error expected")
	viper.Set("broker.clientCert", "dummy-value")
	viper.Set("broker.clientKey", "dummy-value")
	_, err = NewConfig("s3inbox")
	assert.NoError(suite.T(), err)

	viper.Set("broker.vhost", nil)
	config, err = NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "/", config.Broker.Vhost)
}

func (suite *ConfigTestSuite) TestTLSConfigBroker() {
	viper.Set("broker.serverName", "broker")
	viper.Set("broker.ssl", true)
	viper.Set("broker.cacert", certPath+"/ca.crt")
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err := TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsBroker)
	assert.NoError(suite.T(), err)

	viper.Set("broker.verifyPeer", true)
	viper.Set("broker.clientCert", certPath+"/tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsBroker)
	assert.NoError(suite.T(), err)

	viper.Set("broker.clientCert", certPath+"tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.Nil(suite.T(), tlsBroker)
	assert.Error(suite.T(), err)
}

func (suite *ConfigTestSuite) TestTLSConfigProxy() {
	viper.Set("inbox.cacert", certPath+"/ca.crt")
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsProxy, err := TLSConfigProxy(config)
	assert.NotNil(suite.T(), tlsProxy)
	assert.NoError(suite.T(), err)
}

func (suite *ConfigTestSuite) TestDefaultLogLevel() {
	viper.Set("log.level", "test")
	config, err := NewConfig("s3inbox")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), log.TraceLevel, log.GetLevel())
}

func (suite *ConfigTestSuite) TestAPIConfiguration() {
	// At this point we should fail because we lack configuration
	viper.Reset()
	config, err := NewConfig("api")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	// testing deafult values
	suite.SetupTest()
	config, err = NewConfig("api")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.API)
	assert.Equal(suite.T(), "0.0.0.0", config.API.Host)
	assert.Equal(suite.T(), 8080, config.API.Port)
	assert.Equal(suite.T(), true, config.API.Session.Secure)
	assert.Equal(suite.T(), true, config.API.Session.HTTPOnly)
	assert.Equal(suite.T(), "api_session_key", config.API.Session.Name)
	assert.Equal(suite.T(), -1*time.Second, config.API.Session.Expiration)

	viper.Reset()
	suite.SetupTest()
	// over write defaults
	viper.Set("api.port", 8443)
	viper.Set("api.session.secure", false)
	viper.Set("api.session.domain", "test")
	viper.Set("api.session.expiration", 60)

	config, err = NewConfig("api")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.API)
	assert.Equal(suite.T(), "0.0.0.0", config.API.Host)
	assert.Equal(suite.T(), 8443, config.API.Port)
	assert.Equal(suite.T(), false, config.API.Session.Secure)
	assert.Equal(suite.T(), "test", config.API.Session.Domain)
	assert.Equal(suite.T(), 60*time.Second, config.API.Session.Expiration)
}

func (suite *ConfigTestSuite) TestNotifyConfiguration() {
	// At this point we should fail because we lack configuration
	config, err := NewConfig("notify")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")
	viper.Set("broker.exchange", "test")

	viper.Set("smtp.host", "test")
	viper.Set("smtp.port", 456)
	viper.Set("smtp.password", "test")
	viper.Set("smtp.from", "noreply")

	config, err = NewConfig("notify")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)
}

func (suite *ConfigTestSuite) TestSyncConfig() {
	suite.SetupTest()
	// At this point we should fail because we lack configuration
	config, err := NewConfig("backup")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("archive.type", "posix")
	viper.Set("archive.location", "test")
	viper.Set("sync.centerPrefix", "prefix")
	viper.Set("sync.destination.type", "posix")
	viper.Set("sync.destination.location", "test")
	viper.Set("sync.remote.host", "https://test.org")
	viper.Set("sync.remote.user", "test")
	viper.Set("sync.remote.password", "test")
	viper.Set("c4gh.filepath", "/keys/key")
	viper.Set("c4gh.passphrase", "pass")
	viper.Set("c4gh.syncPubKeyPath", "/keys/recipient")
	config, err = NewConfig("sync")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "testhost", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "testuser", config.Broker.User)
	assert.Equal(suite.T(), "testpassword", config.Broker.Password)
	assert.Equal(suite.T(), "testqueue", config.Broker.Queue)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.Posix)
	assert.Equal(suite.T(), "test", config.Archive.Posix.Location)
	assert.NotNil(suite.T(), config.Sync)
	assert.NotNil(suite.T(), config.Sync.Destination.Posix)
	assert.Equal(suite.T(), "test", config.Sync.Destination.Posix.Location)
}
func (suite *ConfigTestSuite) TestGetC4GHPublicKey() {
	pubKey := "-----BEGIN CRYPT4GH PUBLIC KEY-----\nuQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=\n-----END CRYPT4GH PUBLIC KEY-----"
	pubKeyPath, _ := os.MkdirTemp("", "pubkey")
	err := os.WriteFile(pubKeyPath+"/c4gh.pub", []byte(pubKey), 0600)
	assert.NoError(suite.T(), err)

	var kb [32]byte
	k, _ := base64.StdEncoding.DecodeString("uQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=")
	copy(kb[:], k)

	viper.Set("c4gh.syncPubKeyPath", pubKeyPath+"/c4gh.pub")
	pkBytes, err := GetC4GHPublicKey()
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), pkBytes)
	assert.Equal(suite.T(), pkBytes, &kb, "GetC4GHPublicKey didn't return correct pubKey")

	defer os.RemoveAll(pubKeyPath)
}
func (suite *ConfigTestSuite) TestGetC4GHKey() {
	key := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	keyPath, _ := os.MkdirTemp("", "key")
	err := os.WriteFile(keyPath+"/c4gh.key", []byte(key), 0600)
	assert.NoError(suite.T(), err)

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	pkBytes, err := GetC4GHKey()
	assert.EqualError(suite.T(), err, "chacha20poly1305: message authentication failed")
	assert.Nil(suite.T(), pkBytes)

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")
	pkBytes, err = GetC4GHKey()
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), pkBytes)

	defer os.RemoveAll(keyPath)
}

func (suite *ConfigTestSuite) TestConfigSyncAPI() {
	suite.SetupTest()
	noConfig, err := NewConfig("sync-api")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), noConfig)

	viper.Set("sync.api.user", "user")
	viper.Set("sync.api.password", "password")
	config, err := NewConfig("sync-api")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "user", config.SyncAPI.APIUser)
	assert.Equal(suite.T(), "password", config.SyncAPI.APIPassword)

	viper.Set("sync.api.AccessionRouting", "wrong")
	config, err = NewConfig("sync-api")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "wrong", config.SyncAPI.AccessionRouting)
}
