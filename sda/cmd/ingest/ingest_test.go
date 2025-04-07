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

func (ts *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")
	archive := ts.T().TempDir()
	defer os.RemoveAll(archive)
	viper.Set("archive.location", archive)

	tempDir := ts.T().TempDir()
	keyFile1 := fmt.Sprintf("%s/c4gh1.key", tempDir)
	keyFile2 := fmt.Sprintf("%s/c4gh2.key", tempDir)

	publicKey, err := helper.CreatePrivateKeyFile(keyFile1, "test")
	assert.NoError(ts.T(), err)
	// Add only the first public key to the list
	pubKeyList = append(pubKeyList, publicKey)

	_, err = helper.CreatePrivateKeyFile(keyFile2, "test")
	assert.NoError(ts.T(), err)

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

func (ts *TestSuite) TestTryDecrypt_wrongFile() {
	tempDir := ts.T().TempDir()
	err := os.WriteFile(fmt.Sprintf("%s/dummy.file", tempDir), []byte("hello\ngo\n"), 0600)
	assert.NoError(ts.T(), err)

	file, err := os.Open(fmt.Sprintf("%s/dummy.file", tempDir))
	assert.NoError(ts.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(ts.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)
	assert.Len(ts.T(), privateKeys, 2)

	header, err := tryDecrypt(privateKeys[0], buf)
	assert.Nil(ts.T(), header)
	assert.EqualError(ts.T(), err, "not a Crypt4GH file")
}

func (ts *TestSuite) TestTryDecrypt() {
	_, signingKey, err := keys.GenerateKeyPair()
	assert.NoError(ts.T(), err)

	// encrypt test file
	tempDir := ts.T().TempDir()
	unencryptedFile, err := os.CreateTemp(tempDir, "unencryptedFile-")
	assert.NoError(ts.T(), err)

	err = os.WriteFile(unencryptedFile.Name(), []byte("content"), 0600)
	assert.NoError(ts.T(), err)

	encryptedFile, err := os.CreateTemp(tempDir, "encryptedFile-")
	assert.NoError(ts.T(), err)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(encryptedFile, signingKey, pubKeyList, nil)
	assert.NoError(ts.T(), err)

	_, err = io.Copy(crypt4GHWriter, unencryptedFile)
	assert.NoError(ts.T(), err)
	crypt4GHWriter.Close()

	file, err := os.Open(encryptedFile.Name())
	assert.NoError(ts.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(ts.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)

	for i, key := range privateKeys {
		header, err := tryDecrypt(key, buf)
		if i == 0 {
			assert.NoError(ts.T(), err)
			assert.NotNil(ts.T(), header)
		} else {
			assert.Contains(ts.T(), err.Error(), "could not find matching public key heade")
			assert.Nil(ts.T(), header)
		}
	}
}
