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

	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
}

var certPath, rootDir string

func (cts *ConfigTestSuite) SetupTest() {
	_, b, _, _ := runtime.Caller(0)
	rootDir = path.Join(path.Dir(b), "../../../")
	// pwd, _ = os.Getwd()
	certPath, _ = os.MkdirTemp("", "gocerts")
	helper.MakeCerts(certPath)

	rbacFile, err := os.CreateTemp(certPath, "admins")
	assert.NoError(cts.T(), err)
	rbac := []byte(`{"policy":[
{"role":"admin","path":"/c4gh-keys/*","action":"(GET)|(POST)|(PUT)"},
{"role":"submission","path":"/dataset/create","action":"POST"},
{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
{"role":"submission","path":"/file/ingest","action":"POST"},
{"role":"submission","path":"/file/accession","action":"POST"}],
"roles":[{"role":"admin","rolebinding":"submission"},
{"role":"dummy@example.org","rolebinding":"admin"},
{"role":"foo@example.org","rolebinding":"submission"}]}`)
	_, err = rbacFile.Write(rbac)
	assert.NoError(cts.T(), err)

	viper.Set("api.rbacFile", rbacFile.Name())
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
	viper.Set("inbox.type", "s3")
	viper.Set("server.jwtpubkeypath", "testpath")
	viper.Set("log.level", "debug")
}

func (cts *ConfigTestSuite) TearDownTest() {
	viper.Reset()
	defer os.RemoveAll(certPath)
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (cts *ConfigTestSuite) TestNonExistingApplication() {
	expectedError := errors.New("application 'test' doesn't exist")
	config, err := NewConfig("test")
	assert.Nil(cts.T(), config)
	if assert.Error(cts.T(), err) {
		assert.Equal(cts.T(), expectedError, err)
	}
}

func (cts *ConfigTestSuite) TestConfigFile() {
	viper.Set("configFile", rootDir+"/.github/integration/sda/config.yaml")
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(cts.T(), absPath, viper.ConfigFileUsed())
}

func (cts *ConfigTestSuite) TestWrongConfigFile() {
	viper.Set("configFile", rootDir+"/.github/integration/rabbitmq/cega.conf")
	config, err := NewConfig("s3inbox")
	assert.Nil(cts.T(), config)
	assert.Error(cts.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/rabbitmq/cega.conf")
	assert.Equal(cts.T(), absPath, viper.ConfigFileUsed())
}

func (cts *ConfigTestSuite) TestConfigPath() {
	viper.Reset()
	viper.Set("configPath", rootDir+"/.github/integration/sda/")
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	absPath, _ := filepath.Abs(rootDir + "/.github/integration/sda/config.yaml")
	assert.Equal(cts.T(), absPath, viper.ConfigFileUsed())
}

func (cts *ConfigTestSuite) TestNoConfig() {
	viper.Reset()
	config, err := NewConfig("s3inbox")
	assert.Nil(cts.T(), config)
	assert.Error(cts.T(), err)
}

func (cts *ConfigTestSuite) TestMissingRequiredConfVar() {
	for _, requiredConfVar := range requiredConfVars {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("s3inbox")
		assert.Nil(cts.T(), config)
		if assert.Error(cts.T(), err) {
			assert.Equal(cts.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (cts *ConfigTestSuite) TestConfigS3Storage() {
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config.Inbox.S3)
	assert.Equal(cts.T(), "testurl", config.Inbox.S3.URL)
	assert.Equal(cts.T(), "testaccess", config.Inbox.S3.AccessKey)
	assert.Equal(cts.T(), "testsecret", config.Inbox.S3.SecretKey)
	assert.Equal(cts.T(), "testbucket", config.Inbox.S3.Bucket)
}

func (cts *ConfigTestSuite) TestConfigBroker() {
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config.Inbox.S3)
	assert.Equal(cts.T(), "/testvhost", config.Broker.Vhost)
	assert.Equal(cts.T(), false, config.Broker.Ssl)

	viper.Set("broker.ssl", true)
	viper.Set("broker.verifyPeer", true)
	_, err = NewConfig("s3inbox")
	assert.Error(cts.T(), err, "Error expected")
	viper.Set("broker.clientCert", "dummy-value")
	viper.Set("broker.clientKey", "dummy-value")
	_, err = NewConfig("s3inbox")
	assert.NoError(cts.T(), err)

	viper.Set("broker.vhost", nil)
	config, err = NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), "/", config.Broker.Vhost)
}

func (cts *ConfigTestSuite) TestTLSConfigBroker() {
	viper.Set("broker.serverName", "broker")
	viper.Set("broker.ssl", true)
	viper.Set("broker.cacert", certPath+"/ca.crt")
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	tlsBroker, err := TLSConfigBroker(config)
	assert.NotNil(cts.T(), tlsBroker)
	assert.NoError(cts.T(), err)

	viper.Set("broker.verifyPeer", true)
	viper.Set("broker.clientCert", certPath+"/tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.NotNil(cts.T(), tlsBroker)
	assert.NoError(cts.T(), err)

	viper.Set("broker.clientCert", certPath+"tls.crt")
	viper.Set("broker.clientKey", certPath+"/tls.key")
	config, err = NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	tlsBroker, err = TLSConfigBroker(config)
	assert.Nil(cts.T(), tlsBroker)
	assert.Error(cts.T(), err)
}

func (cts *ConfigTestSuite) TestTLSConfigProxy() {
	viper.Set("inbox.cacert", certPath+"/ca.crt")
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	tlsProxy, err := TLSConfigProxy(config)
	assert.NotNil(cts.T(), tlsProxy)
	assert.NoError(cts.T(), err)
}

func (cts *ConfigTestSuite) TestDefaultLogLevel() {
	viper.Set("log.level", "test")
	config, err := NewConfig("s3inbox")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), log.TraceLevel, log.GetLevel())
}

func (cts *ConfigTestSuite) TestAPIConfiguration() {
	// At this point we should fail because we lack configuration
	viper.Reset()
	config, err := NewConfig("api")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), config)

	// testing deafult values
	cts.SetupTest()
	config, err = NewConfig("api")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config.API)
	assert.Equal(cts.T(), "0.0.0.0", config.API.Host)
	assert.Equal(cts.T(), 8080, config.API.Port)
	assert.Equal(cts.T(), true, config.API.Session.Secure)
	assert.Equal(cts.T(), true, config.API.Session.HTTPOnly)
	assert.Equal(cts.T(), "api_session_key", config.API.Session.Name)
	assert.Equal(cts.T(), -1*time.Second, config.API.Session.Expiration)
	rbac, _ := os.ReadFile(viper.GetString("api.rbacFile"))
	assert.Equal(cts.T(), rbac, config.API.RBACpolicy)

	viper.Reset()
	cts.SetupTest()
	// over write defaults
	viper.Set("api.port", 8443)
	viper.Set("api.session.secure", false)
	viper.Set("api.session.domain", "test")
	viper.Set("api.session.expiration", 60)

	config, err = NewConfig("api")
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config.API)
	assert.Equal(cts.T(), "0.0.0.0", config.API.Host)
	assert.Equal(cts.T(), 8443, config.API.Port)
	assert.Equal(cts.T(), false, config.API.Session.Secure)
	assert.Equal(cts.T(), "test", config.API.Session.Domain)
	assert.Equal(cts.T(), 60*time.Second, config.API.Session.Expiration)
}

func (cts *ConfigTestSuite) TestNotifyConfiguration() {
	// At this point we should fail because we lack configuration
	config, err := NewConfig("notify")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), config)

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
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config)
}

func (cts *ConfigTestSuite) TestSyncConfig() {
	cts.SetupTest()
	// At this point we should fail because we lack configuration
	config, err := NewConfig("backup")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), config)

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
	assert.NotNil(cts.T(), config)
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), config.Broker)
	assert.Equal(cts.T(), "testhost", config.Broker.Host)
	assert.Equal(cts.T(), 123, config.Broker.Port)
	assert.Equal(cts.T(), "testuser", config.Broker.User)
	assert.Equal(cts.T(), "testpassword", config.Broker.Password)
	assert.Equal(cts.T(), "testqueue", config.Broker.Queue)
	assert.NotNil(cts.T(), config.Database)
	assert.Equal(cts.T(), "test", config.Database.Host)
	assert.Equal(cts.T(), 123, config.Database.Port)
	assert.Equal(cts.T(), "test", config.Database.User)
	assert.Equal(cts.T(), "test", config.Database.Password)
	assert.Equal(cts.T(), "test", config.Database.Database)
	assert.NotNil(cts.T(), config.Archive)
	assert.NotNil(cts.T(), config.Archive.Posix)
	assert.Equal(cts.T(), "test", config.Archive.Posix.Location)
	assert.NotNil(cts.T(), config.Sync)
	assert.NotNil(cts.T(), config.Sync.Destination.Posix)
	assert.Equal(cts.T(), "test", config.Sync.Destination.Posix.Location)
}
func (cts *ConfigTestSuite) TestGetC4GHPublicKey() {
	pubKey := "-----BEGIN CRYPT4GH PUBLIC KEY-----\nuQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=\n-----END CRYPT4GH PUBLIC KEY-----"
	pubKeyPath, _ := os.MkdirTemp("", "pubkey")
	err := os.WriteFile(pubKeyPath+"/c4gh.pub", []byte(pubKey), 0600)
	assert.NoError(cts.T(), err)

	var kb [32]byte
	k, _ := base64.StdEncoding.DecodeString("uQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=")
	copy(kb[:], k)

	viper.Set("c4gh.syncPubKeyPath", pubKeyPath+"/c4gh.pub")
	pkBytes, err := GetC4GHPublicKey()
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), pkBytes)
	assert.Equal(cts.T(), pkBytes, &kb, "GetC4GHPublicKey didn't return correct pubKey")

	defer os.RemoveAll(pubKeyPath)
}
func (cts *ConfigTestSuite) TestGetC4GHKey() {
	key := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	keyPath, _ := os.MkdirTemp("", "key")
	err := os.WriteFile(keyPath+"/c4gh.key", []byte(key), 0600)
	assert.NoError(cts.T(), err)

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	pkBytes, err := GetC4GHKey()
	assert.EqualError(cts.T(), err, "chacha20poly1305: message authentication failed")
	assert.Nil(cts.T(), pkBytes)

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")
	pkBytes, err = GetC4GHKey()
	assert.NoError(cts.T(), err)
	assert.NotNil(cts.T(), pkBytes)

	defer os.RemoveAll(keyPath)
}

func (cts *ConfigTestSuite) TestGetC4GHprivateKeys_AllOK() {
	keyPath, _ := os.MkdirTemp("", "key")
	keyFile1 := keyPath + "/c4gh1.key"
	keyFile2 := keyPath + "/c4gh2.key"

	_, err := helper.CreatePrivateKeyFile(keyFile1, "test")
	assert.NoError(cts.T(), err)
	_, err = helper.CreatePrivateKeyFile(keyFile2, "test")
	assert.NoError(cts.T(), err)

	viper.Set("c4gh.privateKeys", []C4GHprivateKeyConf{
		{FilePath: keyFile1, Passphrase: "test"},
		{FilePath: keyFile2, Passphrase: "test"},
	})

	privateKeys, err := GetC4GHprivateKeys()
	assert.NoError(cts.T(), err)
	assert.Len(cts.T(), privateKeys, 2)

	defer os.RemoveAll(keyPath)
}

func (cts *ConfigTestSuite) TestGetC4GHprivateKeys_MissingKeyPath() {
	viper.Set("c4gh.privateKeys", []C4GHprivateKeyConf{
		{FilePath: "/non/existent/path1", Passphrase: "test"},
	})

	privateKeys, err := GetC4GHprivateKeys()
	assert.Error(cts.T(), err)
	assert.Contains(cts.T(), err.Error(), "failed to open key file")
	assert.Nil(cts.T(), privateKeys)
}

func (cts *ConfigTestSuite) TestGetC4GHprivateKeys_WrongPassphrase() {
	keyPath, _ := os.MkdirTemp("", "key")
	keyFile := keyPath + "/c4gh1.key"

	_, err := helper.CreatePrivateKeyFile(keyFile, "test")
	assert.NoError(cts.T(), err)

	viper.Set("c4gh.privateKeys", []C4GHprivateKeyConf{
		{FilePath: keyFile, Passphrase: "wrong"},
	})

	privateKeys, err := GetC4GHprivateKeys()
	assert.Error(cts.T(), err)
	assert.Contains(cts.T(), err.Error(), "chacha20poly1305: message authentication faile")
	assert.Nil(cts.T(), privateKeys)

	defer os.RemoveAll(keyPath)
}

func (cts *ConfigTestSuite) TestGetC4GHprivateKeys_InvalidKey() {
	key := "not a valid key"
	keyPath, _ := os.MkdirTemp("", "key")
	keyFile := keyPath + "/c4gh1.key"

	err := os.WriteFile(keyFile, []byte(key), 0600)
	assert.NoError(cts.T(), err)

	viper.Set("c4gh.privateKeys", []C4GHprivateKeyConf{
		{FilePath: keyFile, Passphrase: "wrong"},
	})

	privateKeys, err := GetC4GHprivateKeys()
	assert.Error(cts.T(), err)
	assert.Contains(cts.T(), err.Error(), "read of unrecognized private key format")
	assert.Nil(cts.T(), privateKeys)

	defer os.RemoveAll(keyPath)
}

func (cts *ConfigTestSuite) TestConfigSyncAPI() {
	cts.SetupTest()
	noConfig, err := NewConfig("sync-api")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), noConfig)

	viper.Set("sync.api.user", "user")
	viper.Set("sync.api.password", "password")
	config, err := NewConfig("sync-api")
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), "user", config.SyncAPI.APIUser)
	assert.Equal(cts.T(), "password", config.SyncAPI.APIPassword)

	viper.Set("sync.api.AccessionRouting", "wrong")
	config, err = NewConfig("sync-api")
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), "wrong", config.SyncAPI.AccessionRouting)
}

func (cts *ConfigTestSuite) TestConfigReEncryptServer() {
	cts.SetupTest()
	noConfig, err := NewConfig("reencrypt")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), noConfig)

	key := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	keyPath, _ := os.MkdirTemp("", "key")
	defer os.RemoveAll(keyPath)
	if err := os.WriteFile(keyPath+"/c4gh.key", []byte(key), 0600); err != nil {
		cts.T().FailNow()
	}

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")
	config, err := NewConfig("reencrypt")
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), 50051, config.ReEncrypt.Port)

	viper.Set("grpc.CACert", certPath+"/ca.crt")
	viper.Set("grpc.serverCert", certPath+"/tls.crt")
	viper.Set("grpc.serverKey", certPath+"/tls.key")
	config, err = NewConfig("reencrypt")
	assert.NoError(cts.T(), err)
	assert.Equal(cts.T(), certPath+"/ca.crt", config.ReEncrypt.CACert)
	assert.Equal(cts.T(), certPath+"/tls.crt", config.ReEncrypt.ServerCert)
}

func (cts *ConfigTestSuite) TestConfigAuth_CEGA() {
	cts.SetupTest()

	ECPath, _ := os.MkdirTemp("", "EC")
	if err := helper.CreateECkeys(ECPath, ECPath); err != nil {
		cts.T().FailNow()
	}
	defer os.RemoveAll(ECPath)

	noConfig, err := NewConfig("auth")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), noConfig)

	viper.Set("auth.s3Inbox", "http://inbox:8000")
	viper.Set("auth.publicFile", "no-file")
	viper.Set("auth.cega.authURL", "http://cega/auth")
	viper.Set("auth.cega.id", "CegaID")
	viper.Set("auth.cega.secret", "CegaSecret")
	viper.Set("auth.jwt.Issuer", "http://auth:8080")
	viper.Set("auth.Jwt.privateKey", "nonexistent-key-file")
	viper.Set("auth.Jwt.signatureAlg", "ES256")
	viper.Set("auth.Jwt.tokenTTL", 168)
	_, err = NewConfig("auth")
	assert.ErrorContains(cts.T(), err, "no such file or directory")

	viper.Set("auth.publicFile", ECPath+"/ec.pub")
	viper.Set("auth.Jwt.privateKey", ECPath+"/ec")
	c, err := NewConfig("auth")
	assert.Equal(cts.T(), c.Auth.JwtPrivateKey, fmt.Sprintf("%s/ec", ECPath))
	assert.Equal(cts.T(), c.Auth.JwtTTL, 168)
	assert.NoError(cts.T(), err, "unexpected failure")
}

func (cts *ConfigTestSuite) TestConfigAuth_OIDC() {
	cts.SetupTest()

	ECPath, _ := os.MkdirTemp("", "EC")
	if err := helper.CreateECkeys(ECPath, ECPath); err != nil {
		cts.T().FailNow()
	}
	defer os.RemoveAll(ECPath)

	noConfig, err := NewConfig("auth")
	assert.Error(cts.T(), err)
	assert.Nil(cts.T(), noConfig)

	viper.Set("auth.s3Inbox", "http://inbox:8000")
	viper.Set("auth.publicFile", ECPath+"/ec.pub")
	viper.Set("oidc.id", "oidcTestID")
	viper.Set("oidc.secret", "oidcTestIssuer")
	_, err = NewConfig("auth")
	assert.Error(cts.T(), err)

	viper.Set("oidc.provider", "http://provider:9000")
	viper.Set("oidc.redirectUrl", "http://auth/oidc/login")
	_, err = NewConfig("auth")
	assert.NoError(cts.T(), err, "unexpected failure")
}
