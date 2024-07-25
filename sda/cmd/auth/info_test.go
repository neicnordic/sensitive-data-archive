package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type InfoTests struct {
	pubKeyb64 string
	suite.Suite
	TempDir string
}

func TestInfoTestSuite(t *testing.T) {
	suite.Run(t, new(InfoTests))
}

func (suite *InfoTests) SetupTest() {
	suite.TempDir, _ = os.MkdirTemp("", "key")

	pub, _, err := keys.GenerateKeyPair()
	if err != nil {
		suite.FailNowf("Filed to generate crypt4gh keypair", err.Error())
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, pub); err != nil {
		suite.T().FailNow()
	}
	suite.pubKeyb64 = base64.StdEncoding.EncodeToString(buf.Bytes())

	pubKeyFile, err := os.Create(suite.TempDir + "/pub.key")
	if err != nil {
		suite.T().FailNow()
	}

	_, err = pubKeyFile.Write(buf.Bytes())
	if err != nil {
		suite.T().FailNow()
	}

}

func (suite *InfoTests) TestReadPublicKeyFile() {
	pubKey, err := readPublicKeyFile(suite.TempDir + "/pub.key")
	assert.NoError(suite.T(), err, "Reading public key from disk failed")
	assert.Equal(suite.T(), suite.pubKeyb64, pubKey)
}

func (suite *InfoTests) TearDownTest() {
	os.RemoveAll(suite.TempDir)
}
