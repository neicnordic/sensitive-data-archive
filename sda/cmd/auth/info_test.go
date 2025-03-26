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

func (its *InfoTests) SetupTest() {
	its.TempDir, _ = os.MkdirTemp("", "key")

	pub, _, err := keys.GenerateKeyPair()
	if err != nil {
		its.FailNowf("Filed to generate crypt4gh keypair", err.Error())
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, pub); err != nil {
		its.T().FailNow()
	}
	its.pubKeyb64 = base64.StdEncoding.EncodeToString(buf.Bytes())

	pubKeyFile, err := os.Create(its.TempDir + "/pub.key")
	if err != nil {
		its.T().FailNow()
	}

	_, err = pubKeyFile.Write(buf.Bytes())
	if err != nil {
		its.T().FailNow()
	}
}

func (its *InfoTests) TestReadPublicKeyFile() {
	pubKey, err := readPublicKeyFile(its.TempDir + "/pub.key")
	assert.NoError(its.T(), err, "Reading public key from disk failed")
	assert.Equal(its.T(), its.pubKeyb64, pubKey)
}

func (its *InfoTests) TearDownTest() {
	os.RemoveAll(its.TempDir)
}
