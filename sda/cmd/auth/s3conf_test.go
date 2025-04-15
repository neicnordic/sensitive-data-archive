package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// These are not complete tests of all functions in elixir. New tests should
// be added as the code is updated.

type S3ConfTests struct {
	suite.Suite
}

func TestS3ConfTestSuite(t *testing.T) {
	suite.Run(t, new(S3ConfTests))
}

func (suite *S3ConfTests) SetupTest() {}

func (suite *S3ConfTests) TearDownTest() {}

//nolint:goconst
func (suite *S3ConfTests) TestGetS3ConfigMap() {
	// variable values
	token := "tokenvaluestring"
	inboxHost := "s3://inboxHost"
	user := "s3user"

	// static values

	checkSslCertificate := "False"
	checkSslHostname := "False"
	encoding := "UTF-8"
	encrypt := "False"
	guessMimeType := "True"
	humanReadableSizes := "True"
	chunkSize := 50
	useHTTPS := "True"
	socketTimeout := 30

	s3conf := getS3ConfigMap(token, inboxHost, user)

	assert.Equal(suite.T(), user, s3conf["access_key"], fmt.Sprintf("access_key should be %v", user))
	assert.Equal(suite.T(), user, s3conf["secret_key"], fmt.Sprintf("secret_key should be %v", user))
	assert.Equal(suite.T(), token, s3conf["access_token"], fmt.Sprintf("access_token should be %v", token))
	assert.Equal(suite.T(), checkSslCertificate, s3conf["check_ssl_certificate"], fmt.Sprintf("check_ssl_certificate should be %v", checkSslCertificate))
	assert.Equal(suite.T(), checkSslHostname, s3conf["check_ssl_hostname"], fmt.Sprintf("check_ssl_hostname should be %v", checkSslHostname))
	assert.Equal(suite.T(), encoding, s3conf["encoding"], fmt.Sprintf("encoding should be %v", encoding))
	assert.Equal(suite.T(), encrypt, s3conf["encrypt"], fmt.Sprintf("encrypt should be %v", encrypt))
	assert.Equal(suite.T(), guessMimeType, s3conf["guess_mime_type"], fmt.Sprintf("guess_mime_type should be %v", guessMimeType))
	assert.Equal(suite.T(), inboxHost, s3conf["host_base"], fmt.Sprintf("host_base should be %v", inboxHost))
	assert.Equal(suite.T(), inboxHost, s3conf["host_bucket"], fmt.Sprintf("host_bucket should be %v", inboxHost))
	assert.Equal(suite.T(), humanReadableSizes, s3conf["human_readable_sizes"], fmt.Sprintf("human_readable_sizes should be %v", humanReadableSizes))
	assert.Equal(suite.T(), fmt.Sprintf("%v", chunkSize), s3conf["multipart_chunk_size_mb"], fmt.Sprintf("multipart_chunk_size_mb should be %v", chunkSize))
	assert.Equal(suite.T(), useHTTPS, s3conf["use_https"], fmt.Sprintf("use_https should be '%v'", useHTTPS))
	assert.Equal(suite.T(), fmt.Sprintf("%v", socketTimeout), s3conf["socket_timeout"], fmt.Sprintf("socket_timeout should be %v", socketTimeout))
}
