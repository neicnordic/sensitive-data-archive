package main

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
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

	// Generate a crypth4gh keypair
	publicKey, privateKey, err := keys.GenerateKeyPair()
	assert.NoError(suite.T(), err)
	pubKeyList = append(pubKeyList, publicKey)

	tempDir := suite.T().TempDir()

	// Write the first private key file
	privateKeyFile1, err := os.Create(fmt.Sprintf("%s/c4gh1.key", tempDir))
	assert.NoError(suite.T(), err)
	err = keys.WriteCrypt4GHX25519PrivateKey(privateKeyFile1, privateKey, []byte("c4ghpass"))
	assert.NoError(suite.T(), err)
	privateKeyFile1.Close()

	// Write the second private key file
	privateKeyFile2, err := os.Create(fmt.Sprintf("%s/c4gh2.key", tempDir))
	assert.NoError(suite.T(), err)
	err = keys.WriteCrypt4GHX25519PrivateKey(privateKeyFile2, privateKey, []byte("c4ghpass"))
	assert.NoError(suite.T(), err)
	privateKeyFile2.Close()

	viper.Set("c4gh.keys", []map[string]string{
		{
			"filePath":   fmt.Sprintf("%s/c4gh1.key", tempDir),
			"passphrase": "c4ghpass",
		},
		{
			"filePath":   fmt.Sprintf("%s/c4gh2.key", tempDir),
			"passphrase": "c4ghpass",
		},
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

	keyList, err := config.GetC4GHKeyList()
	assert.NoError(suite.T(), err)

	var decryptionSuccessful bool
	for _, key := range keyList {
		b, err := tryDecrypt(key, buf)
		if b != nil || err == nil {
			decryptionSuccessful = true

			break
		}
		assert.EqualError(suite.T(), err, "not a Crypt4GH file")
	}

	assert.False(suite.T(), decryptionSuccessful, "Decryption should not succeed with any key")
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

	keyList, err := config.GetC4GHKeyList()
	assert.NoError(suite.T(), err)
	for _, key := range keyList {
		header, err := tryDecrypt(key, buf)
		if header != nil && err == nil {

			break
		}
		assert.NoError(suite.T(), err)
		assert.NotNil(suite.T(), header)
	}
}
