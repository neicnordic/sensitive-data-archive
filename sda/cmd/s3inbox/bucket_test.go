package main

import (
	"net/http/httptest"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var ts *httptest.Server

type BucketTestSuite struct {
	suite.Suite
}

func (suite *BucketTestSuite) SetupTest() {
	err := setupFakeS3()
	if err != nil {
		log.Error("Setup of fake s3 failed, bailing out")
		os.Exit(1)
	}

	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", "1234")
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.routingkey", "ingest")
	viper.Set("broker.exchange", "amq.topic")
	viper.Set("broker.vhost", "/")
	viper.Set("aws.url", ts.URL)
	viper.Set("aws.accesskey", "testaccess")
	viper.Set("aws.secretkey", "testsecret")
	viper.Set("aws.bucket", "testbucket")
	viper.Set("server.jwtpubkeypath", "testpath")
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

	if err != nil {
		log.Error("Unexpected error while setting up fake s3")

		return err
	}

	return err
}

func TestBucketTestSuite(t *testing.T) {
	suite.Run(t, new(BucketTestSuite))
}

func (suite *BucketTestSuite) TestBucketPass() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)

	err = checkS3Bucket(config.S3)
	assert.NoError(suite.T(), err)
}

func (suite *BucketTestSuite) TestBucketFail() {
	viper.Set("aws.url", "http://localhost:12345")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)

	err = checkS3Bucket(config.S3)
	assert.Error(suite.T(), err)
}
