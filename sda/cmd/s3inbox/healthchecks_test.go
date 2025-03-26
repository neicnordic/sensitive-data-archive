package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	ProxyTests
}

func TestHealthTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (hts *HealthcheckTestSuite) SetupTest() {
	// Reuse the setup from Proxy
	hts.ProxyTests.SetupTest()
}

func (hts *HealthcheckTestSuite) TearDownTest() {
	// Reuse the teardown from Proxy
	hts.ProxyTests.TearDownTest()
}

func (hts *HealthcheckTestSuite) TestHttpsGetCheck() {
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, hts.messenger, hts.database, new(tls.Config))

	url, _ := p.getS3ReadyPath()
	assert.NoError(hts.T(), p.httpsGetCheck(url))
	assert.Error(hts.T(), p.httpsGetCheck("http://127.0.0.1:8888/nonexistent"), "404 should fail")
}

func (hts *HealthcheckTestSuite) TestS3URL() {
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, hts.messenger, hts.database, new(tls.Config))

	_, err := p.getS3ReadyPath()
	assert.NoError(hts.T(), err)

	p.s3.URL = "://badurl"
	url, err := p.getS3ReadyPath()
	assert.Empty(hts.T(), url)
	assert.Error(hts.T(), err)
}

func (hts *HealthcheckTestSuite) TestHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(hts.DBConf)
	messenger, err := broker.NewMQ(hts.MQConf)
	assert.NoError(hts.T(), err)
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(hts.T(), 200, resp.StatusCode)
}

func (hts *HealthcheckTestSuite) TestClosedDBHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(hts.DBConf)
	messenger, err := broker.NewMQ(hts.MQConf)
	assert.NoError(hts.T(), err)
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// Check that 200 is reported
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(hts.T(), 200, resp.StatusCode)

	// Close connection to DB, check that connection is restored and 200 returned
	w = httptest.NewRecorder()
	p.database.Close()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(hts.T(), 200, resp.StatusCode)
}

func (hts *HealthcheckTestSuite) TestNoS3Healthchecks() {
	// Setup
	db, _ := database.NewSDAdb(hts.DBConf)
	messenger, err := broker.NewMQ(hts.MQConf)
	assert.NoError(hts.T(), err)
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// S3 unavailable, check that 503 is reported
	w := httptest.NewRecorder()
	p.s3.URL = "http://badurl"
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(hts.T(), 503, resp.StatusCode)
}

func (hts *HealthcheckTestSuite) TestNoMQHealthchecks() {
	// Setup
	db, _ := database.NewSDAdb(hts.DBConf)
	messenger, err := broker.NewMQ(hts.MQConf)
	assert.NoError(hts.T(), err)
	p := NewProxy(hts.S3conf, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	// Messenger unavailable, check that 503 is reported
	p.messenger.Conf.Port = 123456
	p.messenger.Connection.Close()
	assert.True(hts.T(), p.messenger.Connection.IsClosed())
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(hts.T(), 503, resp.StatusCode)
}
