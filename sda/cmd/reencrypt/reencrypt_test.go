package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ReEncryptTests struct {
	suite.Suite
	FileData         []byte
	KeyPath          string
	FileHeader       []byte
	UserPrivateKey   [32]byte
	UserPublicKey    [32]byte
	UserPubKeyString string
}

func TestReEncryptTests(t *testing.T) {
	suite.Run(t, new(ReEncryptTests))
}

func (suite *ReEncryptTests) SetupTest() {
	var err error
	log.SetLevel(log.DebugLevel)

	repKey := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	suite.KeyPath, _ = os.MkdirTemp("", "key")
	if err := os.WriteFile(suite.KeyPath+"/c4gh.key", []byte(repKey), 0600); err != nil {
		suite.T().FailNow()
	}

	suite.UserPublicKey, suite.UserPrivateKey, err = keys.GenerateKeyPair()
	if err != nil {
		suite.T().FailNow()
	}

	key, err := os.Create(suite.KeyPath + "/new.key")
	if err != nil {
		suite.T().FailNow()
	}
	if err := keys.WriteCrypt4GHX25519PrivateKey(key, suite.UserPrivateKey, []byte("password")); err != nil {
		suite.T().FailNow()
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, suite.UserPublicKey); err != nil {
		suite.T().FailNow()
	}
	suite.UserPubKeyString = base64.StdEncoding.EncodeToString(buf.Bytes())

	viper.Set("c4gh.filepath", suite.KeyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")

	Conf.ReEncrypt.Crypt4GHKey, err = config.GetC4GHKey()
	if err != nil {
		suite.T().FailNow()
	}

	suite.FileHeader, _ = hex.DecodeString("637279707434676801000000010000006c000000000000007ca283608311dacfc32703a3cc9a2b445c9a417e036ba5943e233cfc65a1f81fdcc35036a584b3f95759114f584d1e81e8cf23a9b9d1e77b9e8f8a8ee8098c2a3e9270fe6872ef9d1c948caf8423efc7ce391081da0d52a49b1e6d0706f267d6140ff12b")
	suite.FileData, _ = hex.DecodeString("e046718f01d52c626276ce5931e10afd99330c4679b3e2a43fdf18146e85bae8eaee83")
	lis, err := net.Listen("tcp", "localhost:50051")
	if err != nil {
		suite.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{})
		if err := s.Serve(lis); err != nil {
			suite.T().Fail()
		}
	}()
}

func (suite *ReEncryptTests) TearDownTest() {
	os.RemoveAll(suite.KeyPath)
}

func (suite *ReEncryptTests) TestReencryptHeader() {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial("localhost:50051", opts...)
	if err != nil {
		suite.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: suite.FileHeader, Publickey: suite.UserPubKeyString})
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "crypt4gh", string(res.Header[:8]))

	hr := bytes.NewReader(res.Header)
	fileStream := io.MultiReader(hr, bytes.NewReader(suite.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, suite.UserPrivateKey, nil)
	assert.NoError(suite.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "content", string(data))
}
