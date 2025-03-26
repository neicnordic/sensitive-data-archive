package main

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

var pubKeyList = [][32]byte{}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")
	archive := suite.T().TempDir()
	defer os.RemoveAll(archive)
	viper.Set("archive.location", archive)

	tempDir := suite.T().TempDir()
	keyFile1 := fmt.Sprintf("%s/c4gh1.key", tempDir)
	keyFile2 := fmt.Sprintf("%s/c4gh2.key", tempDir)

	publicKey, err := helper.CreatePrivateKeyFile(keyFile1, "test")
	assert.NoError(suite.T(), err)
	// Add only the first public key to the list
	pubKeyList = append(pubKeyList, publicKey)

	_, err = helper.CreatePrivateKeyFile(keyFile2, "test")
	assert.NoError(suite.T(), err)

	viper.Set("c4gh.privateKeys", []config.C4GHprivateKeyConf{
		{FilePath: keyFile1, Passphrase: "test"},
		{FilePath: keyFile2, Passphrase: "test"},
	})

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
}

func (suite *TestSuite) TestTryDecrypt_wrongFile() {
	tempDir := suite.T().TempDir()
	err := os.WriteFile(fmt.Sprintf("%s/dummy.file", tempDir), []byte("hello\ngo\n"), 0600)
	assert.NoError(suite.T(), err)

	file, err := os.Open(fmt.Sprintf("%s/dummy.file", tempDir))
	assert.NoError(suite.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(suite.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), privateKeys, 2)

	header, err := tryDecrypt(privateKeys[0], buf)
	assert.Nil(suite.T(), header)
	assert.EqualError(suite.T(), err, "not a Crypt4GH file")
}

func (suite *TestSuite) TestTryDecrypt() {
	_, signingKey, err := keys.GenerateKeyPair()
	assert.NoError(suite.T(), err)

	// encrypt test file
	tempDir := suite.T().TempDir()
	unencryptedFile, err := os.CreateTemp(tempDir, "unencryptedFile-")
	assert.NoError(suite.T(), err)

	err = os.WriteFile(unencryptedFile.Name(), []byte("content"), 0600)
	assert.NoError(suite.T(), err)

	encryptedFile, err := os.CreateTemp(tempDir, "encryptedFile-")
	assert.NoError(suite.T(), err)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(encryptedFile, signingKey, pubKeyList, nil)
	assert.NoError(suite.T(), err)

	_, err = io.Copy(crypt4GHWriter, unencryptedFile)
	assert.NoError(suite.T(), err)
	crypt4GHWriter.Close()

	file, err := os.Open(encryptedFile.Name())
	assert.NoError(suite.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(suite.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(suite.T(), err)

	for i, key := range privateKeys {
		header, err := tryDecrypt(key, buf)
		if i == 0 {
			assert.NoError(suite.T(), err)
			assert.NotNil(suite.T(), header)
		} else {
			assert.Contains(suite.T(), err.Error(), "could not find matching public key heade")
			assert.Nil(suite.T(), header)
		}
	}
}
