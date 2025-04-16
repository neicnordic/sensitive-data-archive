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

func (ts *InfoTests) SetupTest() {
	ts.TempDir, _ = os.MkdirTemp("", "key")

	pub, _, err := keys.GenerateKeyPair()
	if err != nil {
		ts.FailNowf("Filed to generate crypt4gh keypair", err.Error())
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, pub); err != nil {
		ts.T().FailNow()
	}
	ts.pubKeyb64 = base64.StdEncoding.EncodeToString(buf.Bytes())

	pubKeyFile, err := os.Create(ts.TempDir + "/pub.key")
	if err != nil {
		ts.T().FailNow()
	}

	_, err = pubKeyFile.Write(buf.Bytes())
	if err != nil {
		ts.T().FailNow()
	}
}

func (ts *InfoTests) TestReadPublicKeyFile() {
	pubKey, err := readPublicKeyFile(ts.TempDir + "/pub.key")
	assert.NoError(ts.T(), err, "Reading public key from disk failed")
	assert.Equal(ts.T(), ts.pubKeyb64, pubKey)
}

func (ts *InfoTests) TearDownTest() {
	os.RemoveAll(ts.TempDir)
}
