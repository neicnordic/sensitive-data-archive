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
)

var ts *httptest.Server

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

func (suite *TestSuite) TestBucketPass() {
	viper.Set("aws.url", ts.URL)
	viper.Set("aws.accesskey", "fakeaccess")
	viper.Set("aws.secretkey", "testsecret")
	viper.Set("aws.bucket", "testbucket")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)

	err = checkS3Bucket(config.S3)
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestBucketFail() {
	viper.Set("aws.url", "http://localhost:12345")
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)

	err = checkS3Bucket(config.S3)
	assert.Error(suite.T(), err)
}
