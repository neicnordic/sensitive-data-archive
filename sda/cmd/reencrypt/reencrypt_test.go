package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type ReEncryptTests struct {
	suite.Suite
	FileData         []byte
	KeyPath          string
	FileHeader       []byte
	PrivateKeyList   []*[32]byte
	UserPrivateKey   [32]byte
	UserPublicKey    [32]byte
	UserPubKeyString string
}

func TestReEncryptTests(t *testing.T) {
	suite.Run(t, new(ReEncryptTests))
}

func (ts *ReEncryptTests) SetupTest() {
	var err error
	log.SetLevel(log.InfoLevel)

	repKey := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	ts.KeyPath, _ = os.MkdirTemp("", "key")
	if err := os.WriteFile(ts.KeyPath+"/c4gh.key", []byte(repKey), 0600); err != nil {
		ts.T().FailNow()
	}

	ts.UserPublicKey, ts.UserPrivateKey, err = keys.GenerateKeyPair()
	if err != nil {
		ts.T().FailNow()
	}

	key, err := os.Create(ts.KeyPath + "/new.key")
	if err != nil {
		ts.T().FailNow()
	}
	if err := keys.WriteCrypt4GHX25519PrivateKey(key, ts.UserPrivateKey, []byte("password")); err != nil {
		ts.T().FailNow()
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, ts.UserPublicKey); err != nil {
		ts.T().FailNow()
	}
	ts.UserPubKeyString = base64.StdEncoding.EncodeToString(buf.Bytes())

	viper.Set("c4gh.privateKeys", []config.C4GHprivateKeyConf{
		{FilePath: ts.KeyPath + "/c4gh.key", Passphrase: "test"},
	})

	ts.PrivateKeyList, err = config.GetC4GHprivateKeys()
	if err != nil {
		ts.T().FailNow()
	}

	ts.FileHeader, _ = hex.DecodeString("637279707434676801000000010000006c000000000000007ca283608311dacfc32703a3cc9a2b445c9a417e036ba5943e233cfc65a1f81fdcc35036a584b3f95759114f584d1e81e8cf23a9b9d1e77b9e8f8a8ee8098c2a3e9270fe6872ef9d1c948caf8423efc7ce391081da0d52a49b1e6d0706f267d6140ff12b")
	ts.FileData, _ = hex.DecodeString("e046718f01d52c626276ce5931e10afd99330c4679b3e2a43fdf18146e85bae8eaee83")
}

func (ts *ReEncryptTests) TearDownTest() {
	_ = os.RemoveAll(ts.KeyPath)
}

func (ts *ReEncryptTests) TestReencryptHeader() {
	lis, err := net.Listen("tcp", "localhost:50051")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("localhost:50051", opts...)
	if err != nil {
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString})
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "crypt4gh", string(res.Header[:8]))

	hr := bytes.NewReader(res.Header)
	fileStream := io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "content", string(data))
}

// TestReencryptHeader_LengthStableBytesDiffer pins the invariant that download
// clients rely on: re-encrypting the same header for the same recipient key
// returns a header of identical length on every call, but the bytes differ
// because a fresh ephemeral writer keypair and nonce are generated per call.
// Clients (e.g. sda-download-ui) may trust Content-Length for /files/{id}/header
// but must never assume the header bytes are stable across requests; resume
// belongs on /files/{id}/content. See sda/cmd/download/swagger_v2.yml.
func (ts *ReEncryptTests) TestReencryptHeader_LengthStableBytesDiffer() {
	// Bind to an ephemeral port so the test is idempotent under -count and never
	// collides with the other suite cases' fixed ports.
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(ts.T(), err)

	s := grpc.NewServer()
	re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(lis) }()
	defer func() {
		s.Stop()
		// Serve returns once Stop() closes the listener; ErrServerStopped is the
		// expected outcome, anything else is a real server failure.
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			ts.T().Errorf("grpc server Serve failed: %v", err)
		}
	}()

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.NewClient(lis.Addr().String(), opts...)
	require.NoError(ts.T(), err)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)

	first, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString})
	require.NoError(ts.T(), err)
	second, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString})
	require.NoError(ts.T(), err)

	// Length is stable: same header + same recipient key always yields the same
	// size, so clients can trust Content-Length.
	assert.Equal(ts.T(), len(first.Header), len(second.Header), "re-encrypted header length must be stable for a given (header, recipient key)")

	// Bytes are NOT stable: a fresh ephemeral writer keypair and nonce are drawn
	// on every call, so the bytes differ. Clients must not cache or byte-compare
	// the header across requests.
	assert.NotEqual(ts.T(), first.Header, second.Header, "re-encrypted header bytes must differ between calls (fresh ephemeral key and nonce)")

	// Both headers must be valid Crypt4GH, still decrypt the body, and neither
	// may carry a DataEditList packet on the simple path (the packet structure
	// that keeps the length stable).
	for _, h := range [][]byte{first.Header, second.Header} {
		require.GreaterOrEqual(ts.T(), len(h), 8)
		assert.Equal(ts.T(), "crypt4gh", string(h[:8]))

		stream := io.MultiReader(bytes.NewReader(h), bytes.NewReader(ts.FileData))
		c4gh, err := streaming.NewCrypt4GHReader(stream, ts.UserPrivateKey, nil)
		require.NoError(ts.T(), err)
		data, err := io.ReadAll(c4gh)
		require.NoError(ts.T(), err)
		assert.Equal(ts.T(), "content", string(data))

		header, err := headers.NewHeader(bytes.NewReader(h), ts.UserPrivateKey)
		require.NoError(ts.T(), err)
		assert.Nil(ts.T(), header.GetDataEditListHeaderPacket(), "unexpected DataEditList packet on simple re-encrypt")
	}
}

func (ts *ReEncryptTests) TestReencryptHeader_DataEditList() {
	lis, err := net.Listen("tcp", "localhost:50054")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("localhost:50054", opts...)
	if err != nil {
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString, Dataeditlist: []uint64{1, 2, 1, 2}})
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "crypt4gh", string(res.Header[:8]))

	hr := bytes.NewReader(res.Header)
	fileStream := io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "onen", string(data))

	hr = bytes.NewReader(res.Header)
	header, err := headers.NewHeader(hr, ts.UserPrivateKey)
	assert.NoError(ts.T(), err)
	packet := header.GetDataEditListHeaderPacket()
	assert.NotNilf(ts.T(), packet, "DataEditList HeaderPacket not found")
	assert.Equal(ts.T(), uint32(4), packet.NumberLengths)
	assert.Equal(ts.T(), uint64(1), packet.Lengths[0])
	assert.Equal(ts.T(), uint64(2), packet.Lengths[1])
	assert.Equal(ts.T(), uint64(1), packet.Lengths[2])
	assert.Equal(ts.T(), uint64(2), packet.Lengths[3])
	assert.Equal(ts.T(), int(4), len(packet.Lengths))

	res, err = c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString, Dataeditlist: []uint64{}})
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "crypt4gh", string(res.Header[:8]))

	hr = bytes.NewReader(res.Header)
	fileStream = io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err = streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err = io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "content", string(data))

	hr = bytes.NewReader(res.Header)
	header, err = headers.NewHeader(hr, ts.UserPrivateKey)
	assert.NoError(ts.T(), err)
	packet = header.GetDataEditListHeaderPacket()
	assert.Nilf(ts.T(), packet, "DataEditList HeaderPacket found when not expected")
}

func (ts *ReEncryptTests) TestReencryptHeader_BadPubKey() {
	lis, err := net.Listen("tcp", "localhost:50052")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		_ = s.Serve(lis)
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("localhost:50052", opts...)
	if err != nil {
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: "BadKey"})
	assert.Contains(ts.T(), err.Error(), "illegal base64 data")
	assert.Nil(ts.T(), res)
}

func (ts *ReEncryptTests) TestReencryptHeader_NoHeader() {
	lis, err := net.Listen("tcp", "localhost:50053")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		_ = s.Serve(lis)
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("localhost:50053", opts...)
	if err != nil {
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: make([]byte, 0), Publickey: ts.UserPubKeyString})
	assert.Contains(ts.T(), err.Error(), "no header received")
	assert.Nil(ts.T(), res)
}

func (ts *ReEncryptTests) TestReencryptHeader_TLS() {
	certPath := ts.T().TempDir()
	helper.MakeCerts(certPath)
	rootCAs := x509.NewCertPool()
	cacertFile, err := os.ReadFile(certPath + "/ca.crt")
	if err != nil {
		ts.T().FailNow()
	}
	ok := rootCAs.AppendCertsFromPEM(cacertFile)
	if !ok {
		ts.T().FailNow()
	}
	certs, err := tls.LoadX509KeyPair(certPath+"/tls.crt", certPath+"/tls.key")
	if err != nil {
		ts.T().Log(err.Error())
		ts.T().FailNow()
	}

	lis, err := net.Listen("tcp", "localhost:50443")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		serverCreds := credentials.NewTLS(
			&tls.Config{
				Certificates: []tls.Certificate{certs},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				MinVersion:   tls.VersionTLS13,
				ClientCAs:    rootCAs,
			},
		)
		opts := []grpc.ServerOption{grpc.Creds(serverCreds)}
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	clientCreds := credentials.NewTLS(
		&tls.Config{
			Certificates: []tls.Certificate{certs},
			MinVersion:   tls.VersionTLS13,
			RootCAs:      rootCAs,
		},
	)
	conn, err := grpc.NewClient("localhost:50443", grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		ts.T().Log(err.Error())
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString})
	if err != nil {
		ts.T().Log(err.Error())
		ts.T().FailNow()
	}
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), res)
	assert.Equal(ts.T(), "crypt4gh", string(res.Header[:8]))

	hr := bytes.NewReader(res.Header)
	fileStream := io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "content", string(data))
}

func (ts *ReEncryptTests) TestCallReencryptHeader() {
	lis, err := net.Listen("tcp", "localhost:50061")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	grpcConf := config.Grpc{
		Host:    "localhost",
		Port:    50061,
		Timeout: 30,
	}
	res, err := re.CallReencryptHeader(ts.FileHeader, ts.UserPubKeyString, grpcConf)
	assert.NoError(ts.T(), err)

	assert.Equal(ts.T(), "crypt4gh", string(res[:8]))

	hr := bytes.NewReader(res)
	fileStream := io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "content", string(data))
}

func (ts *ReEncryptTests) TestCallReencryptHeaderTLS() {
	certPath := ts.T().TempDir()
	helper.MakeCerts(certPath)
	rootCAs := x509.NewCertPool()
	cacertFile, err := os.ReadFile(certPath + "/ca.crt")
	if err != nil {
		ts.T().FailNow()
	}
	ok := rootCAs.AppendCertsFromPEM(cacertFile)
	if !ok {
		ts.T().FailNow()
	}
	certs, err := tls.LoadX509KeyPair(certPath+"/tls.crt", certPath+"/tls.key")
	if err != nil {
		ts.T().Log(err.Error())
		ts.T().FailNow()
	}

	lis, err := net.Listen("tcp", "localhost:50062")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		serverCreds := credentials.NewTLS(
			&tls.Config{
				Certificates: []tls.Certificate{certs},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				MinVersion:   tls.VersionTLS13,
				ClientCAs:    rootCAs,
			},
		)
		opts := []grpc.ServerOption{grpc.Creds(serverCreds)}
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	clientCreds := credentials.NewTLS(
		&tls.Config{
			Certificates: []tls.Certificate{certs},
			MinVersion:   tls.VersionTLS13,
			RootCAs:      rootCAs,
		},
	)

	grpcConf := config.Grpc{
		ClientCreds: clientCreds,
		Host:        "localhost",
		Port:        50062,
		Timeout:     30,
	}
	res, err := re.CallReencryptHeader(ts.FileHeader, ts.UserPubKeyString, grpcConf)
	assert.NoError(ts.T(), err)

	assert.Equal(ts.T(), "crypt4gh", string(res[:8]))

	hr := bytes.NewReader(res)
	fileStream := io.MultiReader(hr, bytes.NewReader(ts.FileData))

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, ts.UserPrivateKey, nil)
	assert.NoError(ts.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), "content", string(data))
}

func (ts *ReEncryptTests) TestCallReencryptHeader_ConnectionError() {
	grpcConf := config.Grpc{
		Host:    "locahost",
		Port:    50063,
		Timeout: 30,
	}
	_, err := re.CallReencryptHeader(ts.FileHeader, ts.UserPubKeyString, grpcConf)
	assert.Error(ts.T(), err, "expected a connection error")
}

func (ts *ReEncryptTests) TestCallReencryptHeader_BadInput() {
	lis, err := net.Listen("tcp", "localhost:50064")
	if err != nil {
		ts.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.PrivateKeyList})
		if err := s.Serve(lis); err != nil {
			ts.T().Fail()
		}
	}()

	grpcConf := config.Grpc{
		Host:    "localhost",
		Port:    50064,
		Timeout: 30,
	}

	res, err := re.CallReencryptHeader(ts.FileHeader, "somekey", grpcConf)
	assert.ErrorContains(ts.T(), err, "illegal base64 data")
	assert.Nil(ts.T(), res)
}

func (ts *ReEncryptTests) TestReencryptHeader_NoMatchingKey() {
	lis, err := net.Listen("tcp", "localhost:50065")
	if err != nil {
		ts.T().FailNow()
	}

	var keyList []*[32]byte
	_, testKey, err := keys.GenerateKeyPair()
	if err != nil {
		ts.T().FailNow()
	}
	keyList = append(keyList, (&testKey))

	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: keyList})
		_ = s.Serve(lis)
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient("localhost:50065", opts...)
	if err != nil {
		ts.T().FailNow()
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	c := re.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &re.ReencryptRequest{Oldheader: ts.FileHeader, Publickey: ts.UserPubKeyString})
	assert.Contains(ts.T(), err.Error(), "reencryption failed, no matching key available")
	assert.Nil(ts.T(), res)
}
