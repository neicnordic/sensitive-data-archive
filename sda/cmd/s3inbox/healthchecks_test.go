package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
	S3Fakeconf storage.S3Conf // fakeserver
	S3conf     storage.S3Conf // actual s3 container
	DBConf     database.DBConf
	fakeServer *FakeServer
	MQConf     broker.MQConf
	messenger  *broker.AMQPBroker
	database   *database.SDAdb
}

func TestHealthTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (ts *HealthcheckTestSuite) SetupTest() {
	ts.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	ts.S3Fakeconf = storage.S3Conf{
		URL:       "http://127.0.0.1",
		Port:      9024,
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}

	// Create a configuration for the fake MQ
	ts.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     MQport,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	ts.messenger = &broker.AMQPBroker{}

	// Create a database configuration for the fake database
	ts.DBConf = database.DBConf{
		Host:       "127.0.0.1",
		Port:       DBport,
		User:       "postgres",
		Password:   "rootpasswd",
		Database:   "sda",
		CACert:     "",
		SslMode:    "disable",
		ClientCert: "",
		ClientKey:  "",
	}

	ts.database = &database.SDAdb{}
}

func (ts *HealthcheckTestSuite) TearDownTest() {
	ts.fakeServer.Close()
	ts.database.Close()
}

func (ts *HealthcheckTestSuite) TestHttpsGetCheck() {
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, ts.messenger, ts.database, new(tls.Config))

	url, _ := p.getS3ReadyPath()
	assert.NoError(ts.T(), p.httpsGetCheck(url))
	assert.Error(ts.T(), p.httpsGetCheck("http://127.0.0.1:8888/nonexistent"), "404 should fail")
}

func (ts *HealthcheckTestSuite) TestS3URL() {
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, ts.messenger, ts.database, new(tls.Config))

	_, err := p.getS3ReadyPath()
	assert.NoError(ts.T(), err)

	p.s3.URL = "://badurl"
	url, err := p.getS3ReadyPath()
	assert.Empty(ts.T(), url)
	assert.Error(ts.T(), err)
}

func (ts *HealthcheckTestSuite) TestHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestClosedDBHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// Check that 200 is reported
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)

	// Close connection to DB, check that connection is restored and 200 returned
	w = httptest.NewRecorder()
	p.database.Close()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoS3Healthchecks() {
	// Setup
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// S3 unavailable, check that 503 is reported
	w := httptest.NewRecorder()
	p.s3.URL = "http://badurl"
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoMQHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.S3Fakeconf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// Messenger unavailable, check that 503 is reported
	p.messenger.Conf.Port = 123456
	p.messenger.Connection.Close()
	assert.True(ts.T(), p.messenger.Connection.IsClosed())
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}
