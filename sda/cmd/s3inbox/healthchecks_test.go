package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
	mockS3Conf     config.S3InboxConf // fakeserver
	s3ClientToMock *s3.Client
	fakeServer     *FakeServer
	MQConf         broker.MQConf
	messenger      *broker.AMQPBroker
	database       database.Database
}

func TestHealthTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (ts *HealthcheckTestSuite) SetupTest() {
	ts.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	ts.mockS3Conf = config.S3InboxConf{
		Endpoint:  "http://127.0.0.1:9024",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	var err error
	ts.s3ClientToMock, err = newS3Client(context.Background(), ts.mockS3Conf)
	if err != nil {
		ts.FailNow(err.Error())
	}

	// Create a configuration for the fake MQ
	ts.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	ts.messenger = &broker.AMQPBroker{}

	ts.database, err = postgres.NewPostgresSQLDatabase(
		postgres.Host("127.0.0.1"),
		postgres.Port(dbPort),
		postgres.User("postgres"),
		postgres.Password("rootpasswd"),
		postgres.DatabaseName("sda"),
		postgres.Schema("sda"),
		postgres.CACert(""),
		postgres.SslMode("disable"),
		postgres.ClientCert(""),
		postgres.ClientKey(""),
	)
	if err != nil {
		ts.FailNow("failed to connect to database", err)
	}
}

func (ts *HealthcheckTestSuite) TearDownTest() {
	ts.fakeServer.Close()
	_ = ts.database.Close()
}

func (ts *HealthcheckTestSuite) TestHttpsGetCheck() {
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, ts.messenger, ts.database, new(tls.Config))

	// Success path (200 OK)
	url, _ := p.getS3ReadyPath()
	assert.NoError(ts.T(), p.httpsGetCheck(context.Background(), url))

	// HTTP Non-200 Status Response (Server reachable, but path not found)
	mock404Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
	}))
	defer mock404Server.Close()

	err := p.httpsGetCheck(context.Background(), mock404Server.URL+"/nonexistent")
	assert.Error(ts.T(), err)
	assert.Contains(ts.T(), err.Error(), fmt.Sprintf("S3 check to %s/nonexistent returned status 404", mock404Server.URL))
	assert.Contains(ts.T(), err.Error(), "<Code>NoSuchKey</Code>")

	// HTTP Non-200 response with S3 error body extraction
	mockS3ErrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Access Denied.</Message></Error>`))
	}))
	defer mockS3ErrorServer.Close()

	err = p.httpsGetCheck(context.Background(), mockS3ErrorServer.URL)
	assert.Error(ts.T(), err)
	assert.Contains(ts.T(), err.Error(), fmt.Sprintf("S3 check to %s returned status 403", mockS3ErrorServer.URL))
	assert.Contains(ts.T(), err.Error(), "<Code>AccessDenied</Code>")

	// Timeout / Context Deadline Exceeded (S3 hangs)
	mockHangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(16 * time.Second) // Exceeds the 15s context timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer mockHangingServer.Close()

	err = p.httpsGetCheck(context.Background(), mockHangingServer.URL)
	assert.Error(ts.T(), err)
	assert.Contains(ts.T(), err.Error(), fmt.Sprintf("S3 backend check failed for %s", mockHangingServer.URL))
	assert.Contains(ts.T(), err.Error(), "context deadline exceeded")
}

func (ts *HealthcheckTestSuite) TestS3URL() {
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, ts.messenger, ts.database, new(tls.Config))

	_, err := p.getS3ReadyPath()
	assert.NoError(ts.T(), err)

	p.s3Conf.Endpoint = "://badurl"
	url, err := p.getS3ReadyPath()
	assert.Empty(ts.T(), url)
	assert.Error(ts.T(), err)
}

func (ts *HealthcheckTestSuite) TestHealthchecks() {
	// Setup
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, ts.database, new(tls.Config))

	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestClosedDBHealthchecks() {
	// Setup
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, ts.database, new(tls.Config))

	// Check that 200 is reported
	w := httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)

	pgContainer, _ := dockerPool.ContainerByName(postgresContainerName)
	if pgContainer == nil {
		ts.FailNow("postgres container not found")
	}

	networks, err := dockerPool.NetworksByName("bridge")
	if err != nil || len(networks) != 1 {
		ts.FailNow("failed to find docker network: bridge")
	}

	if err := pgContainer.DisconnectFromNetwork(&networks[0]); err != nil {
		ts.FailNow("failed to disconnect postgres from bridge network")
	}

	w = httptest.NewRecorder()

	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)

	if err := pgContainer.ConnectToNetwork(&networks[0]); err != nil {
		ts.FailNow("failed to connect postgres from bridge network")
	}

	w = httptest.NewRecorder()
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp = w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 200, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoS3Healthchecks() {
	// Setup
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, ts.database, new(tls.Config))

	// S3 unavailable, check that 503 is reported
	w := httptest.NewRecorder()
	p.s3Conf.Endpoint = "http://badurl"
	p.CheckHealth(w, httptest.NewRequest(http.MethodGet, "https://dummy/health", nil))
	resp := w.Result()
	defer resp.Body.Close()
	assert.Equal(ts.T(), 503, resp.StatusCode)
}

func (ts *HealthcheckTestSuite) TestNoMQHealthchecks() {
	// Setup
	messenger, err := broker.NewMQ(ts.MQConf)
	assert.NoError(ts.T(), err)
	p := NewProxy(ts.mockS3Conf, ts.s3ClientToMock, &helper.AlwaysAllow{}, messenger, ts.database, new(tls.Config))

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
