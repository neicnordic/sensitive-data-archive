package storage

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/johannesboyne/gofakes3"
	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

// posixType is the configuration type used for posix backends
const posixType = "posix"

// s3Type is the configuration type used for posix backends
const s3Type = "s3"

var testS3Conf = S3Conf{
	"http://127.0.0.1",
	9000,
	"accesskey",
	"secretkey",
	"bucket",
	"region",
	10,
	5 * 1024 * 1024,
	"../../README.md",
	2 * time.Second}

var testConf = Conf{posixType, testS3Conf, testPosixConf}

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

	testConf.Type = s3Type
	s, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend s3 failed")

	assert.IsType(t, p, &posixBackend{}, "Wrong type from NewBackend with posix")
	assert.IsType(t, s, &s3Backend{}, "Wrong type from NewBackend with S3")

	// test some extra ssl handling
	testConf.S3.Cacert = "/dev/null"
	s, err = NewBackend(testConf)
	assert.Nil(t, err, "Backend s3 failed")
	assert.IsType(t, s, &s3Backend{}, "Wrong type from NewBackend with S3")
}

func TestMain(m *testing.M) {

	err := setupFakeS3()

	if err != nil {
		log.Error("Setup of fake s3 failed, bailing out")
		os.Exit(1)
	}

	ret := m.Run()
	ts.Close()
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
	assert.NotNil(t, reader, "Got a nil reader for posix")

	if reader == nil {
		t.Error("reader that should be usable is not, bailing out")

		return
	}

	var readBackBuffer [4096]byte
	readBack, err := reader.Read(readBackBuffer[0:4096])

	assert.Equal(t, len(writeData), readBack, "did not read back data as expected")
	assert.Equal(t, writeData, readBackBuffer[:readBack], "did not read back data as expected")
	assert.Nil(t, err, "unexpected error when reading back data")

	size, err := backend.GetFileSize(writable)
	assert.Nil(t, err, "posix NewFileReader failed when it should work")
	assert.NotNil(t, size, "Got a nil size for posix")

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
	assert.Nil(t, err, "posix NewFileWriter failed when it shouldn't")

	written, err := writer.Write(writeData)

	assert.Nil(t, err, "Failure when writing to s3 writer")
	assert.Equal(t, len(writeData), written, "Did not write all writeData")
	writer.Close()

	reader, err := s3back.NewFileReader(s3Creatable)
	assert.Nil(t, err, "s3 NewFileReader failed when it should work")
	assert.NotNil(t, reader, "Got a nil reader for s3")

	size, err := s3back.GetFileSize(s3Creatable)
	assert.Nil(t, err, "s3 GetFileSize failed when it should work")
	assert.Equal(t, int64(len(writeData)), size, "Got an incorrect file size")

	if reader == nil {
		t.Error("reader that should be usable is not, bailing out")

		return
	}

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
