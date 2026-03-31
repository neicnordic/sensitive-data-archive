package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
	mockS3Conf     config.S3InboxConf
	s3ClientToMock *s3.Client
	DBConf         database.DBConf
	fakeServer     *FakeServer
	MQConf         broker.MQConf
	messenger      *broker.AMQPBroker
	database       *database.SDAdb
}

func TestHealthTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (ts *HealthcheckTestSuite) SetupTest() {
	ts.fakeServer = startFakeServer("9024")

	ts.mockS3Conf = config.S3InboxConf{
		Endpoint:  "http://127.0.0.1:9024",
		ReadyPath: "/health",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	var err error
	ts.s3ClientToMock, err = newS3Client(context.TODO(), ts.mockS3Conf)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     MQport,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	ts.messenger, err = broker.NewMQ(ts.MQConf)
	if err != nil {
		ts.T().Fatalf("failed to connect to messenger")
	}

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

	ts.database, err = database.NewSDAdb(ts.DBConf)
	if err != nil {
		ts.T().Fatalf("failed to connect to db")
	}
}

func (ts *HealthcheckTestSuite) TearDownTest() {
	ts.fakeServer.Close()
	ts.database.Close()
	ts.T().Log("tearing down stuff")
}

func (ts *HealthcheckTestSuite) TestHealthChecks_Ready() {
	ts.T().Log("Test Ready Probe")
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, ts.messenger, ts.database, new(tls.Config))

	w := httptest.NewRecorder()
	p.ReadinessHandler(w, httptest.NewRequest(http.MethodGet, "", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestClosedDBHealthchecks() {
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	w := httptest.NewRecorder()
	p.ReadinessHandler(w, httptest.NewRequest(http.MethodGet, "http://dummy/health/ready", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)

	w = httptest.NewRecorder()
	p.database.Close()
	p.ReadinessHandler(w, httptest.NewRequest(http.MethodGet, "http://dummy/health/ready", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoS3Healthchecks() {
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	w := httptest.NewRecorder()
	p.s3Conf.Endpoint = "http://badurl"
	p.ReadinessHandler(w, httptest.NewRequest(http.MethodGet, "https://dummy/ready", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoMQHealthchecks() {
	db, _ := database.NewSDAdb(ts.DBConf)
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, db, new(tls.Config))

	p.messenger.Conf.Port = 123456
	p.messenger.Connection.Close()
	assert.True(ts.T(), p.messenger.Connection.IsClosed())
	w := httptest.NewRecorder()
	p.ReadinessHandler(w, httptest.NewRequest(http.MethodGet, "https://dummy/ready", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}
