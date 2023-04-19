package main

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HealthcheckTestSuite struct {
	suite.Suite
}

func TestHealthcheckTestSuite(t *testing.T) {
	suite.Run(t, new(HealthcheckTestSuite))
}

func (suite *HealthcheckTestSuite) TestHttpsGetCheck() {
	h := NewHealthCheck(8888,
		S3Config{url: "http://localhost:8080", readypath: "/"},
		BrokerConfig{host: "localhost", port: "8080"},
		new(tls.Config))

	assert.NoError(suite.T(), h.httpsGetCheck("https://www.nbis.se", 10*time.Second)())
	assert.Error(suite.T(), h.httpsGetCheck("https://www.nbis.se/nonexistent", 5*time.Second)(), "404 should fail")
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

	h := NewHealthCheck(8888,
		S3Config{url: "http://localhost:8080", readypath: "/"},
		BrokerConfig{host: "localhost", port: "8080"},
		new(tls.Config))

	go h.RunHealthChecks()

	time.Sleep(100 * time.Millisecond)

	res, err := http.Get("http://localhost:8888/ready?full=1")
	if err != nil {
		log.Fatal(err)
	}
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		suite.T().Errorf("Response code was %v; want 200", res.StatusCode)
	}

	ts.Close()
}
