package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/config"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
}

func TestHealthcheckTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (suite *HealthcheckTestSuite) SetupTest() {
	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", "8080")
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.routingkey", "ingest")
	viper.Set("broker.exchange", "sda")
	viper.Set("broker.vhost", "sda")
	viper.Set("inbox.url", "http://localhost:8080")
	viper.Set("inbox.accesskey", "testaccess")
	viper.Set("inbox.secretkey", "testsecret")
	viper.Set("inbox.bucket", "testbucket")
	viper.Set("server.jwtpubkeypath", "testpath")
}

func (suite *HealthcheckTestSuite) TestHttpsGetCheck() {
	l, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		log.Fatal(err)
	}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewUnstartedServer(foo)
	ts.Listener.Close()
	ts.Listener = l
	ts.Start()
	db, _, _ := sqlmock.New()
	conf, err := config.NewConfig("s3inbox")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), conf)
	h := NewHealthCheck(8888,
		db,
		conf,
		new(tls.Config))

	assert.NoError(suite.T(), h.httpsGetCheck("http://127.0.0.1:9999", 10*time.Second)())
	assert.Error(suite.T(), h.httpsGetCheck("http://127.0.0.1:8888/nonexistent", 5*time.Second)(), "404 should fail")
}

func (suite *HealthcheckTestSuite) TestHealthchecks() {
	l, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		log.Fatal(err)
	}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewUnstartedServer(foo)
	ts.Listener.Close()
	ts.Listener = l
	ts.Start()

	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	mock.ExpectPing()
	conf, err := config.NewConfig("s3inbox")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), conf)
	h := NewHealthCheck(5555,
		db,
		conf,
		new(tls.Config))

	go h.RunHealthChecks()

	time.Sleep(100 * time.Millisecond)

	res, err := http.Get("http://localhost:5555/ready?full=1")
	if err != nil {
		log.Fatal(err)
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		suite.T().Errorf("Response code was %v; want 200", res.StatusCode)
		suite.T().Errorf("Response: %s", b)
	}

	// test head request
	res, err = http.Head("http://localhost:5555")
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		suite.T().Errorf("Response code was %v; want 200", res.StatusCode)
	}

	ts.Close()
}

func (suite *HealthcheckTestSuite) TestBadHealthchecks() {
	l, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		log.Fatal(err)
	}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewUnstartedServer(foo)
	ts.Listener.Close()
	ts.Listener = l
	ts.Start()

	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	conf, err := config.NewConfig("s3inbox")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), conf)
	h := NewHealthCheck(7777,
		db,
		conf,
		new(tls.Config))

	go h.RunHealthChecks()

	time.Sleep(100 * time.Millisecond)

	// Mock DB failure, check that 503 is reported
	mock.ExpectPing().WillReturnError(fmt.Errorf("dbDown"))
	res, err := http.Head("http://localhost:7777")
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusServiceUnavailable {
		suite.T().Errorf("Response code was %v; want 503", res.StatusCode)
	}

	// DB fixed, check that 200 is reported
	mock.ExpectPing()

	res, err = http.Head("http://localhost:7777")
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		suite.T().Errorf("Response code was %v; want 200", res.StatusCode)
	}

	// DB ok, s3 and broker down, check that 503 is reported
	mock.ExpectPing()
	ts.Close()

	res, err = http.Head("http://localhost:7777")
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	if res.StatusCode != http.StatusServiceUnavailable {
		suite.T().Errorf("Response code was %v; want 503", res.StatusCode)
	}

}
