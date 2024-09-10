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

func (suite *HealthcheckTestSuite) SetupTest() {
	// Reuse the setup from Proxy
	suite.ProxyTests.SetupTest()
}

func (suite *HealthcheckTestSuite) TearDownTest() {
	// Reuse the teardown from Proxy
	suite.ProxyTests.TearDownTest()
}

func (suite *HealthcheckTestSuite) TestHttpsGetCheck() {
	p := NewProxy(suite.S3conf, &helper.AlwaysAllow{}, suite.messenger, suite.database, new(tls.Config))

	url := p.getS3ReadyPath()
	assert.NoError(suite.T(), p.httpsGetCheck(url))
	assert.Error(suite.T(), p.httpsGetCheck("http://127.0.0.1:8888/nonexistent"), "404 should fail")
}

func (suite *HealthcheckTestSuite) TestHealthchecks() {
	// Setup
	database, _ := database.NewSDAdb(suite.DBConf)
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	p := NewProxy(suite.S3conf, &helper.AlwaysAllow{}, messenger, database, new(tls.Config))

	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(suite.T(), 200, resp.StatusCode)

}

func (suite *HealthcheckTestSuite) TestClosedDBHealthchecks() {
	// Setup
	database, _ := database.NewSDAdb(suite.DBConf)
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	p := NewProxy(suite.S3conf, &helper.AlwaysAllow{}, messenger, database, new(tls.Config))

	// Check that 200 is reported
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(suite.T(), 200, resp.StatusCode)

	// Close connection to DB, check that connection is restored and 200 returned
	w = httptest.NewRecorder()
	p.database.Close()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(suite.T(), 200, resp.StatusCode)
}

func (suite *HealthcheckTestSuite) TestNoS3Healthchecks() {
	// Setup
	database, _ := database.NewSDAdb(suite.DBConf)
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	p := NewProxy(suite.S3conf, &helper.AlwaysAllow{}, messenger, database, new(tls.Config))

	// S3 unavailable, check that 503 is reported
	w := httptest.NewRecorder()
	p.s3.URL = "http://badurl"
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(suite.T(), 503, resp.StatusCode)
}

func (suite *HealthcheckTestSuite) TestNoMQHealthchecks() {
	// Setup
	database, _ := database.NewSDAdb(suite.DBConf)
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	p := NewProxy(suite.S3conf, &helper.AlwaysAllow{}, messenger, database, new(tls.Config))

	// Messenger unavailable, check that 503 is reported
	p.messenger.Conf.Port = 123456
	p.messenger.Connection.Close()
	assert.True(suite.T(), p.messenger.Connection.IsClosed())
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(suite.T(), 503, resp.StatusCode)
}
