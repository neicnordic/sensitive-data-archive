package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gliderlabs/ssh"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/pkg/sftp"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/johannesboyne/gofakes3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	log "github.com/sirupsen/logrus"
)

// posixType is the configuration type used for posix backends
const posixType = "posix"

// s3Type is the configuration type used for s3 backends
const s3Type = "s3"

// sftpType is the configuration type used for sftp backends
const sftpType = "sftp"

var testS3Conf = S3Conf{
	"http://127.0.0.1",
	9000,
	"accesskey",
	"secretkey",
	"bucket",
	"region",
	10,
	5 * 1024 * 1024,
	"../../dev_utils/certs/ca.pem",
	2 * time.Second}

var testConf = Conf{posixType, testS3Conf, testPosixConf, testSftpConf}

var posixDoesNotExist = "/this/does/not/exist"
var posixNotCreatable = posixDoesNotExist

var ts *httptest.Server

var s3DoesNotExist = "nothing such"
var s3Creatable = "somename"

var writeData = []byte("this is a test")

var cleanupFilesBack [1000]string
var cleanupFiles = cleanupFilesBack[0:0]

var testPosixConf = posixConf{
	"/"}

var testSftpConf = SftpConf{
	"localhost",
	"6222",
	"user",
	"to/be/updated",
	"test",
	"",
}

// HoneyPot encapsulates the initialized mock sftp server struct
type HoneyPot struct {
	server *ssh.Server
}

var hp *HoneyPot

func writeName() (name string, err error) {
	f, err := os.CreateTemp("", "writablefile")

	if err != nil {
		return "", err
	}

	name = f.Name()

	// Add to cleanup
	cleanupFiles = append(cleanupFiles, name)

	return name, err
}

func doCleanup() {
	for _, name := range cleanupFiles {
		os.Remove(name)
	}

	cleanupFiles = cleanupFilesBack[0:0]
}
func TestNewBackend(t *testing.T) {

	testConf.Type = posixType
	p, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend posix failed")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	testConf.Type = sftpType
	sf, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend sftp failed")
	assert.NotZero(t, buf.Len(), "Expected warning missing")

	// update host key from server for later use
	rgx := regexp.MustCompile(`\\\"(.*?)\\"`)
	testConf.SFTP.HostKey = strings.Trim(rgx.FindString(buf.String()), "\\\"")
	buf.Reset()

	testConf.Type = s3Type
	s, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend s3 failed")

	assert.IsType(t, p, &posixBackend{}, "Wrong type from NewBackend with posix")
	assert.IsType(t, s, &s3Backend{}, "Wrong type from NewBackend with S3")
	assert.IsType(t, sf, &sftpBackend{}, "Wrong type from NewBackend with SFTP")

	// test some extra ssl handling
	testConf.S3.Cacert = "/dev/null"
	s, err = NewBackend(testConf)
	assert.Nil(t, err, "Backend s3 failed")
	assert.IsType(t, s, &s3Backend{}, "Wrong type from NewBackend with S3")
}

func TestMain(m *testing.M) {

	err := setupFakeS3()

	if err != nil {
		log.Errorf("Setup of fake s3 failed, bailing out: %v", err)
		os.Exit(1)
	}

	err = setupMockSFTP()

	if err != nil {
		log.Errorf("Setup of mock sftp failed, bailing out: %v", err)
		os.Exit(1)
	}

	ret := m.Run()
	ts.Close()
	hp.server.Close()
	os.Remove(testConf.SFTP.PemKeyPath)
	os.Exit(ret)
}

func TestPosixBackend(t *testing.T) {

	defer doCleanup()
	testConf.Type = posixType
	backend, err := NewBackend(testConf)
	assert.Nil(t, err, "POSIX backend failed unexpectedly")

	var buf bytes.Buffer

	assert.IsType(t, backend, &posixBackend{}, "Wrong type from NewBackend with posix")

	log.SetOutput(os.Stdout)

	writable, err := writeName()
	if err != nil {
		t.Error("could not find a writable name, bailing out from test")

		return
	}

	writer, err := backend.NewFileWriter(writable)

	assert.NotNil(t, writer, "Got a nil reader for writer from posix")
	assert.Nil(t, err, "posix NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)

	assert.Nil(t, err, "Failure when writing to posix writer")
	assert.Equal(t, len(writeData), written, "Did not write all writeData")
	writer.Close()

	log.SetOutput(&buf)
	writer, err = backend.NewFileWriter(posixNotCreatable)

	assert.Nil(t, writer, "Got a non-nil reader for writer from posix")
	assert.NotNil(t, err, "posix NewFileWriter worked when it shouldn't")
	assert.NotZero(t, buf.Len(), "Expected warning missing")

	log.SetOutput(os.Stdout)

	reader, err := backend.NewFileReader(writable)
	assert.Nil(t, err, "posix NewFileReader failed when it should work")
	require.NotNil(t, reader, "Reader that should be usable is not, bailing out")

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(t, len(writeData), readBack, "did not read back data as expected")
	assert.Equal(t, writeData, readBackBuffer[:readBack], "did not read back data as expected")
	assert.Nil(t, err, "unexpected error when reading back data")

	size, err := backend.GetFileSize(writable)
	assert.Nil(t, err, "posix NewFileReader failed when it should work")
	assert.NotNil(t, size, "Got a nil size for posix")

	err = backend.RemoveFile(writable)
	assert.Nil(t, err, "posix RemoveFile failed when it should work")

	log.SetOutput(&buf)

	reader, err = backend.NewFileReader(posixDoesNotExist)
	assert.NotNil(t, err, "posix NewFileReader worked when it should not")
	assert.Nil(t, reader, "Got a non-nil reader for posix")
	assert.NotZero(t, buf.Len(), "Expected warning missing")

	buf.Reset()

	_, err = backend.GetFileSize(posixDoesNotExist) // nolint
	assert.NotNil(t, err, "posix GetFileSize worked when it should not")
	assert.NotZero(t, buf.Len(), "Expected warning missing")

	buf.Reset()

}

func setupFakeS3() (err error) {
	// fake s3

	if ts != nil {
		// Setup done already?
		return
	}

	backend := s3mem.New()
	faker := gofakes3.New(backend)
	ts = httptest.NewServer(faker.Server())

	portAt := strings.LastIndex(ts.URL, ":")

	testConf.S3.URL = ts.URL[:portAt]
	testConf.S3.Port, err = strconv.Atoi(ts.URL[portAt+1:])
	testConf.Type = s3Type

	if err != nil {
		log.Error("Unexpected error while setting up fake s3")

		return err
	}

	backEnd, err := NewBackend(testConf)
	if err != nil {
		return err
	}

	s3back := backEnd.(*s3Backend)

	_, err = s3back.Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(testConf.S3.Bucket)})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {

			if aerr.Code() != s3.ErrCodeBucketAlreadyOwnedByYou &&
				aerr.Code() != s3.ErrCodeBucketAlreadyExists {
				log.Error("Unexpected issue while creating bucket: ", err)
			} else {
				// Do not flag an error for this
				err = nil
			}
		}
	}

	return err
}

func TestS3Fail(t *testing.T) {

	testConf.Type = s3Type

	tmp := testConf.S3.URL

	defer func() { testConf.S3.URL = tmp }()
	testConf.S3.URL = "file://tmp/"
	_, err := NewBackend(testConf)
	assert.NotNil(t, err, "Backend worked when it should not")

	var dummyBackend *s3Backend
	reader, err := dummyBackend.NewFileReader("/")
	assert.NotNil(t, err, "NewFileReader worked when it should not")
	assert.Nil(t, reader, "Got a Reader when expected not to")

	writer, err := dummyBackend.NewFileWriter("/")
	assert.NotNil(t, err, "NewFileWriter worked when it should not")
	assert.Nil(t, writer, "Got a Writer when expected not to")

	_, err = dummyBackend.GetFileSize("/")
	assert.NotNil(t, err, "GetFileSize worked when it should not")

	err = dummyBackend.RemoveFile("/")
	assert.NotNil(t, err, "RemoveFile worked when it should not")
}

func TestPOSIXFail(t *testing.T) {
	testConf.Type = posixType

	tmp := testConf.Posix.Location

	defer func() { testConf.Posix.Location = tmp }()

	testConf.Posix.Location = "/thisdoesnotexist"
	backEnd, err := NewBackend(testConf)
	assert.NotNil(t, err, "Backend worked when it should not")
	assert.Nil(t, backEnd, "Got a backend when expected not to")

	testConf.Posix.Location = "/etc/passwd"

	backEnd, err = NewBackend(testConf)
	assert.NotNil(t, err, "Backend worked when it should not")
	assert.Nil(t, backEnd, "Got a backend when expected not to")

	var dummyBackend *posixBackend
	reader, err := dummyBackend.NewFileReader("/")
	assert.NotNil(t, err, "NewFileReader worked when it should not")
	assert.Nil(t, reader, "Got a Reader when expected not to")

	writer, err := dummyBackend.NewFileWriter("/")
	assert.NotNil(t, err, "NewFileWriter worked when it should not")
	assert.Nil(t, writer, "Got a Writer when expected not to")

	_, err = dummyBackend.GetFileSize("/")
	assert.NotNil(t, err, "GetFileSize worked when it should not")

	err = dummyBackend.RemoveFile("/")
	assert.NotNil(t, err, "RemoveFile worked when it should not")
}

// Initializes a mock sftp server instance
func setupMockSFTP() error {

	password := testConf.SFTP.PemKeyPass
	addr := testConf.SFTP.Host + ":" + testConf.SFTP.Port

	// Key-pair generation
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	publicRsaKey, err := cryptossh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}
	// pem.Block
	privBlock := pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   x509.MarshalPKCS1PrivateKey(privateKey),
	}
	block, err := x509.EncryptPEMBlock(rand.Reader, privBlock.Type, privBlock.Bytes, []byte(password), x509.PEMCipherAES256) //nolint:staticcheck
	if err != nil {
		return err
	}

	// Private key file in PEM format
	privateKeyBytes := pem.EncodeToMemory(block)

	// Create temp key file and update config
	fi, err := os.CreateTemp("", "sftp-key")
	testConf.SFTP.PemKeyPath = fi.Name()
	if err != nil {
		return err
	}
	err = os.WriteFile(testConf.SFTP.PemKeyPath, privateKeyBytes, 0600)
	if err != nil {
		return err
	}

	// Initialize a sftp honeypot instance
	hp = NewHoneyPot(addr, publicRsaKey)

	// Start the server in the background
	go func() {
		if err := hp.server.ListenAndServe(); err != nil {
			log.Panic(err)
		}
	}()

	return err
}

// NewHoneyPot takes in IP address to be used for sftp honeypot
func NewHoneyPot(addr string, key ssh.PublicKey) *HoneyPot {
	return &HoneyPot{
		server: &ssh.Server{
			Addr: addr,
			SubsystemHandlers: map[string]ssh.SubsystemHandler{
				"sftp": func(sess ssh.Session) {
					debugStream := io.Discard
					serverOptions := []sftp.ServerOption{
						sftp.WithDebug(debugStream),
					}
					server, err := sftp.NewServer(
						sess,
						serverOptions...,
					)
					if err != nil {
						log.Errorf("sftp server init error: %v\n", err)

						return
					}
					if err := server.Serve(); err != io.EOF {
						log.Errorf("sftp server completed with error: %v\n", err)
					}
				},
			},
			PublicKeyHandler: func(ctx ssh.Context, key ssh.PublicKey) bool {
				return true
			},
		},
	}
}

func TestSftpFail(t *testing.T) {

	testConf.Type = sftpType

	// test connection
	tmpHost := testConf.SFTP.Host

	testConf.SFTP.Host = "nonexistenthost"
	_, err := NewBackend(testConf)
	assert.NotNil(t, err, "Backend worked when it should not")

	var dummyBackend *sftpBackend
	reader, err := dummyBackend.NewFileReader("/")
	assert.NotNil(t, err, "NewFileReader worked when it should not")
	assert.Nil(t, reader, "Got a Reader when expected not to")

	writer, err := dummyBackend.NewFileWriter("/")
	assert.NotNil(t, err, "NewFileWriter worked when it should not")
	assert.Nil(t, writer, "Got a Writer when expected not to")

	_, err = dummyBackend.GetFileSize("/")
	assert.NotNil(t, err, "GetFileSize worked when it should not")

	err = dummyBackend.RemoveFile("/")
	assert.NotNil(t, err, "RemoveFile worked when it should not")
	assert.EqualError(t, err, "Invalid sftpBackend")
	testConf.SFTP.Host = tmpHost

	// wrong key password
	tmpKeyPass := testConf.SFTP.PemKeyPass
	testConf.SFTP.PemKeyPass = "wrongkey"
	_, err = NewBackend(testConf)
	assert.EqualError(t, err, "Failed to parse private key, x509: decryption password incorrect")

	// missing key password
	testConf.SFTP.PemKeyPass = ""
	_, err = NewBackend(testConf)
	assert.EqualError(t, err, "Failed to parse private key, ssh: this private key is passphrase protected")
	testConf.SFTP.PemKeyPass = tmpKeyPass

	// wrong key
	tmpKeyPath := testConf.SFTP.PemKeyPath
	testConf.SFTP.PemKeyPath = "nonexistentkey"
	_, err = NewBackend(testConf)
	testConf.SFTP.PemKeyPath = tmpKeyPath
	assert.EqualError(t, err, "Failed to read from key file, open nonexistentkey: no such file or directory")

	defer doCleanup()
	dummyKeyFile, err := writeName()
	if err != nil {
		t.Error("could not find a writable name, bailing out from test")

		return
	}
	testConf.SFTP.PemKeyPath = dummyKeyFile
	_, err = NewBackend(testConf)
	assert.EqualError(t, err, "Failed to parse private key, ssh: no key found")
	testConf.SFTP.PemKeyPath = tmpKeyPath

	// wrong host key
	tmpHostKey := testConf.SFTP.HostKey
	testConf.SFTP.HostKey = "wronghostkey"
	_, err = NewBackend(testConf)
	assert.ErrorContains(t, err, "Failed to start ssh connection, ssh: handshake failed: host key verification expected")
	testConf.SFTP.HostKey = tmpHostKey
}

func TestS3Backend(t *testing.T) {

	testConf.Type = s3Type
	backend, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend failed")

	s3back := backend.(*s3Backend)

	var buf bytes.Buffer

	assert.IsType(t, s3back, &s3Backend{}, "Wrong type from NewBackend with s3")

	writer, err := s3back.NewFileWriter(s3Creatable)

	assert.NotNil(t, writer, "Got a nil reader for writer from s3")
	assert.Nil(t, err, "s3 NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)

	assert.Nil(t, err, "Failure when writing to s3 writer")
	assert.Equal(t, len(writeData), written, "Did not write all writeData")
	writer.Close()

	reader, err := s3back.NewFileReader(s3Creatable)
	assert.Nil(t, err, "s3 NewFileReader failed when it should work")
	require.NotNil(t, reader, "Reader that should be usable is not, bailing out")

	size, err := s3back.GetFileSize(s3Creatable)
	assert.Nil(t, err, "s3 GetFileSize failed when it should work")
	assert.NotNil(t, size, "Got a nil size for s3")
	assert.Equal(t, int64(len(writeData)), size, "Got an incorrect file size")

	err = s3back.RemoveFile(s3Creatable)
	assert.Nil(t, err, "s3 RemoveFile failed when it should work")

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(t, len(writeData), readBack, "did not read back data as expected")
	assert.Equal(t, writeData, readBackBuffer[:readBack], "did not read back data as expected")

	if err != nil && err != io.EOF {
		assert.Nil(t, err, "unexpected error when reading back data")
	}

	buf.Reset()

	log.SetOutput(&buf)

	if !testing.Short() {
		_, err = backend.GetFileSize(s3DoesNotExist)
		assert.NotNil(t, err, "s3 GetFileSize worked when it should not")
		assert.NotZero(t, buf.Len(), "Expected warning missing")

		buf.Reset()

		reader, err = backend.NewFileReader(s3DoesNotExist)
		assert.NotNil(t, err, "s3 NewFileReader worked when it should not")
		assert.Nil(t, reader, "Got a non-nil reader for s3")
		assert.NotZero(t, buf.Len(), "Expected warning missing")
	}

	log.SetOutput(os.Stdout)

}

func TestSftpBackend(t *testing.T) {

	var buf bytes.Buffer
	log.SetOutput(&buf)

	testConf.Type = sftpType
	backend, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend failed")

	assert.Zero(t, buf.Len(), "Got warning when not expected")
	buf.Reset()

	sftpBack := backend.(*sftpBackend)

	assert.IsType(t, sftpBack, &sftpBackend{}, "Wrong type from NewBackend with sftp")

	var sftpDoesNotExist = "nonexistent/file"
	var sftpCreatable = os.TempDir() + "/this/file/exists"

	writer, err := sftpBack.NewFileWriter(sftpCreatable)
	assert.NotNil(t, writer, "Got a nil reader for writer from sftp")
	assert.Nil(t, err, "sftp NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)
	assert.Nil(t, err, "Failure when writing to sftp writer")
	assert.Equal(t, len(writeData), written, "Did not write all writeData")
	writer.Close()

	reader, err := sftpBack.NewFileReader(sftpCreatable)
	assert.Nil(t, err, "sftp NewFileReader failed when it should work")
	require.NotNil(t, reader, "Reader that should be usable is not, bailing out")

	size, err := sftpBack.GetFileSize(sftpCreatable)
	assert.Nil(t, err, "sftp GetFileSize failed when it should work")
	assert.NotNil(t, size, "Got a nil size for sftp")
	assert.Equal(t, int64(len(writeData)), size, "Got an incorrect file size")

	err = sftpBack.RemoveFile(sftpCreatable)
	assert.Nil(t, err, "sftp RemoveFile failed when it should work")

	err = sftpBack.RemoveFile(sftpDoesNotExist)
	assert.EqualError(t, err, "Failed to remove file with sftp, file does not exist")

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(t, len(writeData), readBack, "did not read back data as expected")
	assert.Equal(t, writeData, readBackBuffer[:readBack], "did not read back data as expected")

	if err != nil && err != io.EOF {
		assert.Nil(t, err, "unexpected error when reading back data")
	}

	if !testing.Short() {
		_, err = backend.GetFileSize(sftpDoesNotExist)
		assert.EqualError(t, err, "Failed to get file size with sftp, file does not exist")

		reader, err = backend.NewFileReader(sftpDoesNotExist)
		assert.EqualError(t, err, "Failed to open file with sftp, file does not exist")
		assert.Nil(t, reader, "Got a non-nil reader for sftp")
	}

}
