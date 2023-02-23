package main

import (
	"io"
	"os"
	"testing"

	"sda-pipeline/internal/config"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")
	viper.Set("archive.location", "../../dev_utils")
	viper.Set("c4gh.filepath", "../../dev_utils/c4gh.sec.pem")
	viper.Set("c4gh.passphrase", "oaagCP1YgAZeEyl2eJAkHv9lkcWXWFgm")

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

	buf := make([]byte, 1024)
	file, err := os.Open("../../dev_utils/README.md")
	assert.NoError(suite.T(), err)

	_, err = io.ReadFull(file, buf)
	assert.NoError(suite.T(), err)

	key, err := config.GetC4GHKey()
	assert.Nil(suite.T(), err)

	b, err := tryDecrypt(key, buf)
	assert.Nil(suite.T(), b)
	assert.EqualError(suite.T(), err, "not a Crypt4GH file")
}

func (suite *TestSuite) TestTryDecrypt() {

	buf := make([]byte, 65*1024)

	file, err := os.Open("../../dev_utils/dummy_data.c4gh")
	assert.NoError(suite.T(), err)

	_, err = io.ReadFull(file, buf)
	assert.NoError(suite.T(), err)

	key, err := config.GetC4GHKey()
	assert.Nil(suite.T(), err)

	data := []byte{99, 114, 121, 112, 116, 52, 103, 104, 1, 0, 0, 0, 1, 0, 0, 0, 108, 0, 0, 0, 0, 0, 0, 0, 106, 241, 64, 122, 188, 116, 101, 107, 137, 19, 167, 211, 35, 196, 191, 211, 11, 247, 200, 202, 53, 159, 116, 174, 53, 53, 122, 206, 242, 157, 197, 7, 55, 153, 226, 7, 236, 93, 2, 43, 38, 1, 52, 5, 133, 255, 8, 37, 101, 229, 95, 191, 245, 182, 205, 187, 190, 107, 18, 160, 208, 161, 158, 243, 37, 162, 25, 248, 182, 35, 68, 50, 94, 34, 200, 210, 106, 142, 130, 228, 95, 5, 63, 77, 206, 225, 12, 14, 196, 187, 158, 70, 109, 82, 83, 241, 57, 220, 212, 190}

	b, err := tryDecrypt(key, buf)
	assert.Equal(suite.T(), b, data)
	assert.NoError(suite.T(), err)
}
