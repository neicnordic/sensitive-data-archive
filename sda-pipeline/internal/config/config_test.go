package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var defaultRequiredConfVars = requiredConfVars

type TestSuite struct {
	suite.Suite
}

func (suite *TestSuite) SetupTest() {
	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")
	viper.Set("broker.exchange", "test")
	viper.Set("db.host", "test")
	viper.Set("db.port", 123)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")
}

func (suite *TestSuite) TearDownTest() {
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestConfigFile() {
	viper.Set("configFile", "test")
	config, err := NewConfig("test")
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), "test", viper.ConfigFileUsed())
}

func (suite *TestSuite) TestNonExistingApplication() {
	expectedError := errors.New("application 'test' doesn't exist")
	config, err := NewConfig("test")
	assert.Nil(suite.T(), config)
	if assert.Error(suite.T(), err) {
		assert.Equal(suite.T(), expectedError, err)
	}
}

func (suite *TestSuite) TestMissingRequiredConfVar() {
	for _, requiredConfVar := range requiredConfVars {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredArchiveS3ConfVar() {
	viper.Set("archive.type", S3)
	viper.Set("archive.url", "test")
	viper.Set("archive.accesskey", "test")
	viper.Set("archive.secretkey", "test")
	viper.Set("archive.bucket", "test")
	for _, requiredConfVar := range append([]string{"archive.url", "archive.accesskey", "archive.secretkey", "archive.bucket"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredArchivePosixConfVar() {
	viper.Set("archive.type", POSIX)
	viper.Set("archive.location", "test")
	for _, requiredConfVar := range append([]string{"archive.location"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredBackupS3ConfVar() {
	viper.Set("backup.type", S3)
	viper.Set("backup.url", "test")
	viper.Set("backup.accesskey", "test")
	viper.Set("backup.secretkey", "test")
	viper.Set("backup.bucket", "test")
	for _, requiredConfVar := range append([]string{"backup.url", "backup.accesskey", "backup.secretkey", "backup.bucket"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredBackupPosixConfVar() {
	viper.Set("backup.type", POSIX)
	viper.Set("backup.location", "test")
	for _, requiredConfVar := range append([]string{"backup.location"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredBackupSftpConfVar() {
	viper.Set("backup.type", SFTP)
	viper.Set("backup.sftp.host", "test")
	viper.Set("backup.sftp.port", "test")
	viper.Set("backup.sftp.userName", "test")
	viper.Set("backup.sftp.pemKeyPath", "test")
	viper.Set("backup.sftp.pemKeyPass", "test")
	for _, requiredConfVar := range append([]string{"backup.sftp.host", "backup.sftp.port", "backup.sftp.userName", "backup.sftp.pemKeyPath", "backup.sftp.pemKeyPass"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredInboxS3ConfVar() {
	viper.Set("inbox.type", S3)
	viper.Set("inbox.url", "test")
	viper.Set("inbox.accesskey", "test")
	viper.Set("inbox.secretkey", "test")
	viper.Set("inbox.bucket", "test")
	for _, requiredConfVar := range append([]string{"inbox.url", "inbox.accesskey", "inbox.secretkey", "inbox.bucket"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestMissingRequiredInboxPosixConfVar() {
	viper.Set("inbox.type", POSIX)
	viper.Set("inbox.location", "test")
	for _, requiredConfVar := range append([]string{"inbox.location"}, requiredConfVars...) {
		requiredConfVarValue := viper.Get(requiredConfVar)
		viper.Set(requiredConfVar, nil)
		expectedError := fmt.Errorf("%s not set", requiredConfVar)
		config, err := NewConfig("test")
		assert.Nil(suite.T(), config)
		if assert.Error(suite.T(), err) {
			assert.Equal(suite.T(), expectedError, err)
		}
		viper.Set(requiredConfVar, requiredConfVarValue)
	}
}

func (suite *TestSuite) TestConfigS3Storage() {
	viper.Set("archive.type", S3)
	viper.Set("archive.url", "test")
	viper.Set("archive.accesskey", "test")
	viper.Set("archive.secretkey", "test")
	viper.Set("archive.bucket", "test")
	viper.Set("archive.port", 123)
	viper.Set("archive.region", "test")
	viper.Set("archive.chunksize", 123)
	viper.Set("archive.cacert", "test")
	viper.Set("inbox.type", S3)
	viper.Set("inbox.url", "test")
	viper.Set("inbox.accesskey", "test")
	viper.Set("inbox.secretkey", "test")
	viper.Set("inbox.bucket", "test")
	viper.Set("inbox.port", 123)
	viper.Set("inbox.region", "test")
	viper.Set("inbox.chunksize", 123)
	viper.Set("inbox.cacert", "test")
	config, err := NewConfig("ingest")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Inbox)
	assert.NotNil(suite.T(), config.Inbox.S3)
	assert.Equal(suite.T(), S3, config.Inbox.Type)
	assert.Equal(suite.T(), "test", config.Inbox.S3.URL)
	assert.Equal(suite.T(), "test", config.Inbox.S3.AccessKey)
	assert.Equal(suite.T(), "test", config.Inbox.S3.SecretKey)
	assert.Equal(suite.T(), "test", config.Inbox.S3.Bucket)
	assert.Equal(suite.T(), 123, config.Inbox.S3.Port)
	assert.Equal(suite.T(), "test", config.Inbox.S3.Region)
	assert.Equal(suite.T(), 128974848, config.Inbox.S3.Chunksize)
	assert.Equal(suite.T(), "test", config.Inbox.S3.Cacert)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.S3)
	assert.Equal(suite.T(), S3, config.Archive.Type)
	assert.Equal(suite.T(), "test", config.Archive.S3.URL)
	assert.Equal(suite.T(), "test", config.Archive.S3.AccessKey)
	assert.Equal(suite.T(), "test", config.Archive.S3.SecretKey)
	assert.Equal(suite.T(), "test", config.Archive.S3.Bucket)
	assert.Equal(suite.T(), 123, config.Archive.S3.Port)
	assert.Equal(suite.T(), "test", config.Archive.S3.Region)
	assert.Equal(suite.T(), 128974848, config.Archive.S3.Chunksize)
	assert.Equal(suite.T(), "test", config.Archive.S3.Cacert)
}

func (suite *TestSuite) TestConfigBackupS3Storage() {
	viper.Set("archive.type", S3)
	viper.Set("archive.url", "test")
	viper.Set("archive.accesskey", "test")
	viper.Set("archive.secretkey", "test")
	viper.Set("archive.bucket", "test")
	viper.Set("archive.port", 123)
	viper.Set("archive.region", "test")
	viper.Set("archive.chunksize", 123)
	viper.Set("archive.cacert", "test")
	viper.Set("backup.type", S3)
	viper.Set("backup.url", "test")
	viper.Set("backup.accesskey", "test")
	viper.Set("backup.secretkey", "test")
	viper.Set("backup.bucket", "test")
	viper.Set("backup.port", 123)
	viper.Set("backup.region", "test")
	viper.Set("backup.chunksize", 123)
	viper.Set("backup.cacert", "test")
	config, err := NewConfig("backup")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.S3)
	assert.Equal(suite.T(), S3, config.Archive.Type)
	assert.Equal(suite.T(), "test", config.Archive.S3.URL)
	assert.Equal(suite.T(), "test", config.Archive.S3.AccessKey)
	assert.Equal(suite.T(), "test", config.Archive.S3.SecretKey)
	assert.Equal(suite.T(), "test", config.Archive.S3.Bucket)
	assert.Equal(suite.T(), 123, config.Archive.S3.Port)
	assert.Equal(suite.T(), "test", config.Archive.S3.Region)
	assert.Equal(suite.T(), 128974848, config.Archive.S3.Chunksize)
	assert.Equal(suite.T(), "test", config.Archive.S3.Cacert)
	assert.NotNil(suite.T(), config.Backup)
	assert.NotNil(suite.T(), config.Backup.S3)
	assert.Equal(suite.T(), S3, config.Backup.Type)
	assert.Equal(suite.T(), "test", config.Backup.S3.URL)
	assert.Equal(suite.T(), "test", config.Backup.S3.AccessKey)
	assert.Equal(suite.T(), "test", config.Backup.S3.SecretKey)
	assert.Equal(suite.T(), "test", config.Backup.S3.Bucket)
	assert.Equal(suite.T(), 123, config.Backup.S3.Port)
	assert.Equal(suite.T(), "test", config.Backup.S3.Region)
	assert.Equal(suite.T(), 128974848, config.Backup.S3.Chunksize)
	assert.Equal(suite.T(), "test", config.Backup.S3.Cacert)
}

func (suite *TestSuite) TestConfigBackupSftpStorage() {
	viper.Set("backup.type", SFTP)
	viper.Set("backup.sftp.host", "test")
	viper.Set("backup.sftp.port", "123")
	viper.Set("backup.sftp.UserName", "test")
	viper.Set("backup.sftp.pemKeyPath", "test")
	viper.Set("backup.sftp.pemKeyPass", "test")
	config, err := NewConfig("backup")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Backup)
	assert.NotNil(suite.T(), config.Backup.SFTP)
	assert.Equal(suite.T(), SFTP, config.Backup.Type)
	assert.Equal(suite.T(), "test", config.Backup.SFTP.Host)
	assert.Equal(suite.T(), "123", config.Backup.SFTP.Port)
	assert.Equal(suite.T(), "test", config.Backup.SFTP.UserName)
	assert.Equal(suite.T(), "test", config.Backup.SFTP.PemKeyPath)
	assert.Equal(suite.T(), "test", config.Backup.SFTP.PemKeyPass)
}

func (suite *TestSuite) TestConfigBroker() {
	viper.Set("broker.durable", true)
	viper.Set("broker.routingerror", "test")
	viper.Set("broker.vhost", "test")
	viper.Set("broker.ssl", true)
	viper.Set("broker.verifyPeer", true)
	_, err := NewConfig("ingest")
	assert.Error(suite.T(), err, "Error expected")
	viper.Set("broker.clientCert", "test")
	viper.Set("broker.clientKey", "test")
	viper.Set("broker.cacert", "test")
	config, err := NewConfig("ingest")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), true, config.Broker.Durable)
	assert.Equal(suite.T(), "/test", config.Broker.Vhost)
	assert.Equal(suite.T(), true, config.Broker.Ssl)
	assert.Equal(suite.T(), "test", config.Broker.ClientCert)
	assert.Equal(suite.T(), "test", config.Broker.ClientKey)
	assert.Equal(suite.T(), "test", config.Broker.CACert)
	assert.Equal(suite.T(), "file://schemas/federated/", config.Broker.SchemasPath)
	viper.Set("schema.type", "standalone")
	viper.Set("broker.vhost", "/test")
	config, _ = NewConfig("ingest")
	assert.Equal(suite.T(), "/test", config.Broker.Vhost)
	assert.Equal(suite.T(), "file://schemas/isolated/", config.Broker.SchemasPath)
	viper.Set("broker.vhost", "")
	config, _ = NewConfig("ingest")
	assert.Equal(suite.T(), "/", config.Broker.Vhost)
}

func (suite *TestSuite) TestConfigDatabase() {
	viper.Set("db.sslmode", "verify-full")
	_, err := NewConfig("ingest")
	assert.Error(suite.T(), err)
	viper.Set("db.clientCert", "test")
	viper.Set("db.clientKey", "test")
	viper.Set("db.cacert", "test")
	config, err := NewConfig("ingest")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "verify-full", config.Database.SslMode)
	assert.Equal(suite.T(), "test", config.Database.ClientCert)
	assert.Equal(suite.T(), "test", config.Database.ClientKey)
	assert.Equal(suite.T(), "test", config.Database.CACert)
}

func (suite *TestSuite) TestMapperConfiguration() {
	config, err := NewConfig("mapper")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("mapper")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")

	// We should still fail here
	config, err = NewConfig("mapper")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	suite.SetupTest()
	// Now we should have enough
	config, err = NewConfig("mapper")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)

}

func (suite *TestSuite) TestFinalizeConfiguration() {
	config, err := NewConfig("finalize")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.Equal(suite.T(), "test", config.Broker.RoutingKey)
	assert.Equal(suite.T(), "test", config.Broker.Exchange)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("finalize")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")

	// We should still fail here
	config, err = NewConfig("finalize")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	suite.SetupTest()
	// Now we should have enough
	config, err = NewConfig("finalize")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)

}

func (suite *TestSuite) TestVerifyConfiguration() {
	viper.Set("archive.location", "test")
	viper.Set("c4gh.filepath", "test")
	viper.Set("c4gh.passphrase", "test")
	config, err := NewConfig("verify")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.Equal(suite.T(), "test", config.Broker.RoutingKey)
	assert.Equal(suite.T(), "test", config.Broker.Exchange)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.Posix)
	assert.Equal(suite.T(), "test", config.Archive.Posix.Location)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("verify")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")

	// We should still fail here
	config, err = NewConfig("verify")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	suite.SetupTest()
	// Now we should have enough
	config, err = NewConfig("verify")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)

}

func (suite *TestSuite) TestIngestConfiguration() {
	viper.Set("inbox.location", "test")
	viper.Set("archive.location", "test")
	viper.Set("c4gh.filepath", "test")
	viper.Set("c4gh.passphrase", "test")
	config, err := NewConfig("ingest")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.Equal(suite.T(), "test", config.Broker.RoutingKey)
	assert.Equal(suite.T(), "test", config.Broker.Exchange)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)
	assert.NotNil(suite.T(), config.Inbox)
	assert.NotNil(suite.T(), config.Inbox.Posix)
	assert.Equal(suite.T(), "test", config.Inbox.Posix.Location)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.Posix)
	assert.Equal(suite.T(), "test", config.Archive.Posix.Location)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("ingest")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")

	// We should still fail here
	config, err = NewConfig("ingest")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	suite.SetupTest()
	// Now we should have enough
	config, err = NewConfig("ingest")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)

}

func (suite *TestSuite) TestInterceptConfiguration() {
	config, err := NewConfig("intercept")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.Equal(suite.T(), "test", config.Broker.RoutingKey)
	assert.Equal(suite.T(), "test", config.Broker.Exchange)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("intercept")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")

	// Now we should have enough
	config, err = NewConfig("intercept")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)
}
func (suite *TestSuite) TestDefaultLogLevel() {
	viper.Set("log.level", "test")
	config, err := NewConfig("test")
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), log.TraceLevel, log.GetLevel())
}

func (suite *TestSuite) TestConfigPath() {
	viper.Set("configPath", "../../dev_utils")
	config, err := NewConfig("test")
	assert.Nil(suite.T(), config)
	assert.Error(suite.T(), err)
	absPath, _ := filepath.Abs("../../dev_utils/config.yaml")
	assert.Equal(suite.T(), absPath, viper.ConfigFileUsed())
}

func (suite *TestSuite) TestGetC4GHKey() {
	viper.Set("c4gh.filepath", "../../dev_utils/c4gh.sec.pem")
	viper.Set("c4gh.passphrase", "oaagCP1YgAZeEyl2eJAkHv9lkcWXWFgm")
	byte, err := GetC4GHKey()
	assert.NotNil(suite.T(), byte)
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestGetC4GHKey_keyError() {

	viper.Set("c4gh.filepath", "/doesnotexist")

	byte, err := GetC4GHKey()
	assert.Nil(suite.T(), byte)
	assert.EqualError(suite.T(), err, "open /doesnotexist: no such file or directory")
}

func (suite *TestSuite) TestGetC4GHKey_passError() {

	viper.Set("c4gh.filepath", "../../dev_utils/c4gh.sec.pem")
	viper.Set("c4gh.passphrase", "asdf")

	key, err := GetC4GHKey()
	assert.Nil(suite.T(), key)
	assert.EqualError(suite.T(), err, "chacha20poly1305: message authentication failed")
}

func (suite *TestSuite) TestGetC4GHPublicKey() {
	viper.Set("c4gh.backupPubKey", "../../dev_utils/c4gh-new.pub.pem")
	byte, err := GetC4GHPublicKey()
	assert.NotNil(suite.T(), byte)
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestGetC4GHPublicKey_keyError() {

	viper.Set("c4gh.backupPubKey", "/doesnotexist")

	byte, err := GetC4GHPublicKey()
	assert.Nil(suite.T(), byte)
	assert.EqualError(suite.T(), err, "open /doesnotexist: no such file or directory")
}

func (suite *TestSuite) TestBackupConfiguration() {
	viper.Set("archive.location", "test")
	viper.Set("backup.location", "test")
	config, err := NewConfig("backup")
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config.Broker)
	assert.Equal(suite.T(), "test", config.Broker.Host)
	assert.Equal(suite.T(), 123, config.Broker.Port)
	assert.Equal(suite.T(), "test", config.Broker.User)
	assert.Equal(suite.T(), "test", config.Broker.Password)
	assert.Equal(suite.T(), "test", config.Broker.Queue)
	assert.Equal(suite.T(), "test", config.Broker.RoutingKey)
	assert.Equal(suite.T(), "test", config.Broker.Exchange)
	assert.NotNil(suite.T(), config.Database)
	assert.Equal(suite.T(), "test", config.Database.Host)
	assert.Equal(suite.T(), 123, config.Database.Port)
	assert.Equal(suite.T(), "test", config.Database.User)
	assert.Equal(suite.T(), "test", config.Database.Password)
	assert.Equal(suite.T(), "test", config.Database.Database)
	assert.NotNil(suite.T(), config.Archive)
	assert.NotNil(suite.T(), config.Archive.Posix)
	assert.Equal(suite.T(), "test", config.Archive.Posix.Location)
	assert.NotNil(suite.T(), config.Backup)
	assert.NotNil(suite.T(), config.Backup.Posix)
	assert.Equal(suite.T(), "test", config.Backup.Posix.Location)

	// Clear variables
	viper.Reset()
	requiredConfVars = defaultRequiredConfVars

	// At this point we should fail because we lack configuration
	config, err = NewConfig("backup")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")
	viper.Set("broker.exchange", "test")

	// We should still fail here
	config, err = NewConfig("backup")
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), config)

	suite.SetupTest()
	// Now we should have enough
	config, err = NewConfig("backup")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)
}

func (suite *TestSuite) TestCopyHeader() {
	viper.Set("backup.copyHeader", "true")
	cHeader := CopyHeader()
	assert.Equal(suite.T(), cHeader, true, "The CopyHeader does not work")
}

func (suite *TestSuite) TestAPIConfiguration() {
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

func (suite *TestSuite) TestNotifyConfiguration() {
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
