package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	log "github.com/sirupsen/logrus"
)

type StorageTestSuite struct {
	suite.Suite
}

var testConf = Conf{}
var sshPath string
var s3Port, sftpPort int
var writeData = []byte("this is a test")

const posixType = "posix"
const s3Type = "s3"
const sftpType = "sftp"

func TestMain(m *testing.M) {
	sshPath, _ = os.MkdirTemp("", "ssh")
	if err := helper.CreateSSHKey(sshPath); err != nil {
		log.Panicf("Failed to create SSH keys, reason: %v", err.Error())
	}

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Panicf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Panicf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	sftp, err := pool.RunWithOptions(&dockertest.RunOptions{
		Name:       "sftp",
		Repository: "atmoz/sftp",
		Tag:        "latest",
		Cmd:        []string{"user:test:1001::share"},
		Mounts: []string{
			fmt.Sprintf("%s/id_rsa.pub:/home/user/.ssh/keys/id_rsa.pub", sshPath),
			fmt.Sprintf("%s/id_rsa:/etc/ssh/ssh_host_rsa_key", sshPath),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Panicf("Could not start resource: %s", err)
	}

	// sftpHostAndPort := sftp.GetHostPort("22/tcp")
	sftpPort, _ = strconv.Atoi(sftp.GetPort("22/tcp"))

	// pulls an image, creates a container based on it and runs it
	minio, err := pool.RunWithOptions(&dockertest.RunOptions{
		Name:       "s3",
		Repository: "minio/minio",
		Tag:        "RELEASE.2023-05-18T00-05-36Z",
		Cmd:        []string{"server", "/data", "--console-address", ":9001"},
		Env: []string{
			"MINIO_ROOT_USER=access",
			"MINIO_ROOT_PASSWORD=secretKey",
			"MINIO_SERVER_URL=http://127.0.0.1:9000",
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Panicf("Could not start resource: %s", err)
	}

	s3HostAndPort := minio.GetHostPort("9000/tcp")
	s3Port, _ = strconv.Atoi(minio.GetPort("9000/tcp"))

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+s3HostAndPort+"/minio/health/live", http.NoBody)
	if err != nil {
		log.Panic(err)
	}

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(minio); err != nil {
			log.Panicf("Could not purge resource: %s", err)
		}
		log.Panicf("Could not connect to minio: %s", err)
	}

	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(minio); err != nil {
		log.Panicf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(sftp); err != nil {
		log.Panicf("Could not purge resource: %s", err)
	}

	os.RemoveAll(sshPath)

	os.Exit(code)
}

func TestStorageTestSuite(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}

func (ts *StorageTestSuite) SetupTest() {
	testS3Conf := S3Conf{
		"http://127.0.0.1",
		s3Port,
		"access",
		"secretKey",
		"bucket",
		"region",
		10,
		5 * 1024 * 1024,
		"",
		2 * time.Second,
		"",
	}

	testSftpConf := SftpConf{
		"localhost",
		strconv.Itoa(sftpPort),
		"user",
		fmt.Sprintf("%s/id_rsa", sshPath),
		"password",
		"",
	}

	testPosixConf := posixConf{
		os.TempDir(),
	}

	testConf = Conf{posixType, testS3Conf, testPosixConf, testSftpConf}
}

func (ts *StorageTestSuite) TestNewBackend() {
	testConf.Type = posixType
	p, err := NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend posix failed")
	assert.IsType(ts.T(), p, &posixBackend{}, "Wrong type from NewBackend with posix")

	var buf bytes.Buffer
	log.SetOutput(&buf)

	testConf.Type = sftpType
	sf, err := NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend sftp failed")
	assert.NotZero(ts.T(), buf.Len(), "Expected warning missing")
	assert.IsType(ts.T(), sf, &sftpBackend{}, "Wrong type from NewBackend with SFTP")
	buf.Reset()

	testConf.Type = s3Type
	s, err := NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend s3 failed")
	assert.IsType(ts.T(), s, &s3Backend{}, "Wrong type from NewBackend with S3")

	// test some extra ssl handling
	testConf.S3.CAcert = "/dev/null"
	s, err = NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend s3 failed")
	assert.IsType(ts.T(), s, &s3Backend{}, "Wrong type from NewBackend with S3")
}

func (ts *StorageTestSuite) TestNewS3Client() {
	c, err := NewS3Client(context.TODO(), testConf.S3)
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), c)
}

func (ts *StorageTestSuite) TestCheckS3Bucket() {
	ctx := context.TODO()

	s3, err := newS3Backend(ctx, testConf.S3)
	assert.NoError(ts.T(), err)
	assert.NoError(ts.T(), CheckS3Bucket(ctx, testConf.S3.Bucket, s3.Client))

	testConf.S3.URL = "file://tmp/"
	bad, err := newS3Backend(ctx, testConf.S3)
	assert.Error(ts.T(), err)
	err = CheckS3Bucket(ctx, testConf.S3.Bucket, bad.Client)
	assert.Error(ts.T(), err)
}

func (ts *StorageTestSuite) TestPosixBackend() {
	posixPath, _ := os.MkdirTemp("", "posix")
	defer os.RemoveAll(posixPath)
	testConf.Type = posixType
	testConf.Posix = posixConf{posixPath}
	backend, err := NewBackend(context.TODO(), testConf)
	assert.Nil(ts.T(), err, "POSIX backend failed unexpectedly")

	log.SetOutput(os.Stdout)

	writer, err := backend.NewFileWriter(context.TODO(), "testFile")
	assert.NotNil(ts.T(), writer, "Got a nil reader for writer from posix")
	assert.NoError(ts.T(), err, "posix NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)
	assert.NoError(ts.T(), err, "Failure when writing to posix writer")
	assert.Equal(ts.T(), len(writeData), written, "Did not write all writeData")
	writer.Close()

	reader, err := backend.NewFileReader(context.TODO(), "testFile")
	assert.Nil(ts.T(), err, "posix NewFileReader failed when it should work")
	assert.NotNil(ts.T(), reader, "Reader that should be usable is nosuite.T(), bailing out")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	writer, err = backend.NewFileWriter(context.TODO(), "posix/Not/Creatable")
	assert.Nil(ts.T(), writer, "Got a non-nil reader for writer from posix")
	assert.Error(ts.T(), err, "posix NewFileWriter worked when it shouldn't")
	assert.NotZero(ts.T(), buf.Len(), "Expected warning missing")
	buf.Reset()
	log.SetOutput(os.Stdout)

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(ts.T(), len(writeData), readBack, "did not read back data as expected")
	assert.Equal(ts.T(), writeData, readBackBuffer[:readBack], "did not read back data as expected")
	assert.Nil(ts.T(), err, "unexpected error when reading back data")

	size, err := backend.GetFileSize(context.TODO(), "testFile", false)
	assert.Nil(ts.T(), err, "posix NewFileReader failed when it should work")
	assert.NotNil(ts.T(), size, "Got a nil size for posix")

	err = backend.RemoveFile(context.TODO(), "testFile")
	assert.Nil(ts.T(), err, "posix RemoveFile failed when it should work")

	log.SetOutput(&buf)
	reader, err = backend.NewFileReader(context.TODO(), "posixDoesNotExist")
	assert.Error(ts.T(), err, "posix NewFileReader worked when it should not")
	assert.Nil(ts.T(), reader, "Got a non-nil reader for posix")
	assert.NotZero(ts.T(), buf.Len(), "Expected warning missing")

	buf.Reset()
	_, err = backend.GetFileSize(context.TODO(), "posixDoesNotExist", false)
	assert.Error(ts.T(), err, "posix GetFileSize worked when it should not")
	assert.NotZero(ts.T(), buf.Len(), "Expected warning missing")

	log.SetOutput(os.Stdout)

	testConf.Posix.Location = "/thisdoesnotexist"
	backEnd, err := NewBackend(context.TODO(), testConf)
	assert.NotNil(ts.T(), err, "Backend worked when it should not")
	assert.Nil(ts.T(), backEnd, "Got a backend when expected not to")

	testConf.Posix.Location = "/etc/passwd"

	backEnd, err = NewBackend(context.TODO(), testConf)
	assert.NotNil(ts.T(), err, "Backend worked when it should not")
	assert.Nil(ts.T(), backEnd, "Got a backend when expected not to")

	var dummyBackend *posixBackend
	failReader, err := dummyBackend.NewFileReader(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "NewFileReader worked when it should not")
	assert.Nil(ts.T(), failReader, "Got a Reader when expected not to")

	failWriter, err := dummyBackend.NewFileWriter(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "NewFileWriter worked when it should not")
	assert.Nil(ts.T(), failWriter, "Got a Writer when expected not to")

	_, err = dummyBackend.GetFileSize(context.TODO(), "/", false)
	assert.NotNil(ts.T(), err, "GetFileSize worked when it should not")

	err = dummyBackend.RemoveFile(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "RemoveFile worked when it should not")
}

func (ts *StorageTestSuite) TestS3Backend() {
	testConf.Type = s3Type
	s3back, err := NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend failed")

	writer, err := s3back.NewFileWriter(context.TODO(), "s3Creatable")
	assert.NotNil(ts.T(), writer, "Got a nil reader for writer from s3")
	assert.Nil(ts.T(), err, "s3 NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)
	assert.Nil(ts.T(), err, "Failure when writing to s3 writer")
	assert.Equal(ts.T(), len(writeData), written, "Did not write all writeData")
	writer.Close()
	// sleep to allow the write to complete, otherwise the next step will fail due to timing issues.
	time.Sleep(1 * time.Second)

	reader, err := s3back.NewFileReader(context.TODO(), "s3Creatable")
	assert.NoError(ts.T(), err, "s3 NewFileReader failed when it should work")
	assert.NotNil(ts.T(), reader, "Reader that should be usable is not, bailing out")

	size, err := s3back.GetFileSize(context.TODO(), "s3Creatable", false)
	assert.Nil(ts.T(), err, "s3 GetFileSize failed when it should work")
	assert.NotNil(ts.T(), size, "Got a nil size for s3")
	assert.Equal(ts.T(), int64(len(writeData)), size, "Got an incorrect file size")

	// make sure expectDelay=true works
	// delete file and make sure the file size can not be retrieved anymore
	err = s3back.RemoveFile(context.TODO(), "s3Creatable")
	assert.Nil(ts.T(), err, "s3 RemoveFile failed when it should work")
	_, err = s3back.GetFileSize(context.TODO(), "s3Creatable", true)
	assert.NotNil(ts.T(), err, "s3 GetFileSize worked when it should not")
	assert.Error(ts.T(), err)
	// rewrite file, do not wait before retrieving file size
	writer, err = s3back.NewFileWriter(context.TODO(), "s3Creatable")
	assert.Nil(ts.T(), err, "s3 NewFileWriter failed when it shouldn't")
	written, err = writer.Write(writeData)
	assert.Equal(ts.T(), len(writeData), written, "Did not write all writeData")
	assert.Nil(ts.T(), err, "Failure when writing to s3 writer")
	writer.Close()
	size, err = s3back.GetFileSize(context.TODO(), "s3Creatable", true)
	assert.Nil(ts.T(), err, "s3 GetFileSize with expected delay failed when it should work")
	assert.NotNil(ts.T(), size, "Got a nil size for s3")
	assert.Equal(ts.T(), int64(len(writeData)), size, "Got an incorrect file size")

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])
	assert.Equal(ts.T(), len(writeData), readBack, "did not read back data as expected")
	assert.Equal(ts.T(), writeData, readBackBuffer[:readBack], "did not read back data as expected")
	if err != nil && err != io.EOF {
		assert.Nil(ts.T(), err, "unexpected error when reading back data")
	}

	err = s3back.RemoveFile(context.TODO(), "s3Creatable")
	assert.Nil(ts.T(), err, "s3 RemoveFile failed when it should work")

	_, err = s3back.GetFileSize(context.TODO(), "s3DoesNotExist", false)
	assert.NotNil(ts.T(), err, "s3 GetFileSize worked when it should not")
	assert.Error(ts.T(), err)

	reader, err = s3back.NewFileReader(context.TODO(), "s3DoesNotExist")
	assert.NotNil(ts.T(), err, "s3 NewFileReader worked when it should not")
	assert.Error(ts.T(), err)
	assert.Nil(ts.T(), reader, "Got a non-nil reader for s3")

	testConf.S3.URL = "file://tmp/"
	_, err = NewBackend(context.TODO(), testConf)
	assert.Error(ts.T(), err, "Backend worked when it should not")
}

func (ts *StorageTestSuite) TestSftpBackend() {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	testConf.Type = sftpType
	sftpBack, err := NewBackend(context.TODO(), testConf)
	assert.NoError(ts.T(), err, "Backend failed")
	buf.Reset()

	var sftpDoesNotExist = "nonexistent/file"
	var sftpCreatable = "/share/file/exists"

	writer, err := sftpBack.NewFileWriter(context.TODO(), sftpCreatable)
	assert.NotNil(ts.T(), writer, "Got a nil reader for writer from sftp")
	assert.Nil(ts.T(), err, "sftp NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)
	assert.Nil(ts.T(), err, "Failure when writing to sftp writer")
	assert.Equal(ts.T(), len(writeData), written, "Did not write all writeData")
	writer.Close()

	reader, err := sftpBack.NewFileReader(context.TODO(), sftpCreatable)
	assert.Nil(ts.T(), err, "sftp NewFileReader failed when it should work")
	assert.NotNil(ts.T(), reader, "Reader that should be usable is not, bailing out")

	size, err := sftpBack.GetFileSize(context.TODO(), sftpCreatable, false)
	assert.Nil(ts.T(), err, "sftp GetFileSize failed when it should work")
	assert.NotNil(ts.T(), size, "Got a nil size for sftp")
	assert.Equal(ts.T(), int64(len(writeData)), size, "Got an incorrect file size")

	err = sftpBack.RemoveFile(context.TODO(), sftpCreatable)
	assert.Nil(ts.T(), err, "sftp RemoveFile failed when it should work")

	err = sftpBack.RemoveFile(context.TODO(), sftpDoesNotExist)
	assert.ErrorContains(ts.T(), err, "file does not exist")

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(ts.T(), len(writeData), readBack, "did not read back data as expected")
	assert.Equal(ts.T(), writeData, readBackBuffer[:readBack], "did not read back data as expected")

	if err != nil && err != io.EOF {
		assert.Nil(ts.T(), err, "unexpected error when reading back data")
	}

	_, err = sftpBack.GetFileSize(context.TODO(), sftpDoesNotExist, false)
	assert.EqualError(ts.T(), err, "failed to get file size with sftp, file does not exist")
	reader, err = sftpBack.NewFileReader(context.TODO(), sftpDoesNotExist)
	assert.EqualError(ts.T(), err, "failed to open file with sftp, file does not exist")
	assert.Nil(ts.T(), reader, "Got a non-nil reader for sftp")

	// wrong host key
	testConf.SFTP.HostKey = "wronghostkey"
	_, err = NewBackend(context.TODO(), testConf)
	assert.ErrorContains(ts.T(), err, "failed to start ssh connection, ssh: handshake failed: host key verification expected")

	// wrong key password
	testConf.SFTP.PemKeyPass = "wrongkey"
	_, err = NewBackend(context.TODO(), testConf)
	assert.EqualError(ts.T(), err, "failed to parse private key, x509: decryption password incorrect")

	// missing key password
	testConf.SFTP.PemKeyPass = ""
	_, err = NewBackend(context.TODO(), testConf)
	assert.EqualError(ts.T(), err, "failed to parse private key, ssh: this private key is passphrase protected")

	// wrong key
	testConf.SFTP.PemKeyPath = "nonexistentkey"
	_, err = NewBackend(context.TODO(), testConf)
	assert.EqualError(ts.T(), err, "failed to read from key file, open nonexistentkey: no such file or directory")

	f, _ := os.CreateTemp(sshPath, "dummy")
	testConf.SFTP.PemKeyPath = f.Name()
	_, err = NewBackend(context.TODO(), testConf)
	assert.EqualError(ts.T(), err, "failed to parse private key, ssh: no key found")

	testConf.SFTP.Host = "nonexistenthost"
	_, err = NewBackend(context.TODO(), testConf)
	assert.NotNil(ts.T(), err, "Backend worked when it should not")

	var dummyBackend *sftpBackend
	failReader, err := dummyBackend.NewFileReader(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "NewFileReader worked when it should not")
	assert.Nil(ts.T(), failReader, "Got a Reader when expected not to")

	failWriter, err := dummyBackend.NewFileWriter(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "NewFileWriter worked when it should not")
	assert.Nil(ts.T(), failWriter, "Got a Writer when expected not to")

	_, err = dummyBackend.GetFileSize(context.TODO(), "/", false)
	assert.NotNil(ts.T(), err, "GetFileSize worked when it should not")

	err = dummyBackend.RemoveFile(context.TODO(), "/")
	assert.NotNil(ts.T(), err, "RemoveFile worked when it should not")
	assert.EqualError(ts.T(), err, "invalid sftpBackend")
}
