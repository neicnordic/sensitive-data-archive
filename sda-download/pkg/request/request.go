package request

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// Client stores a HTTP client, so that it doesn't need to be initialised on every request
var Client *http.Client

// InitialiseClient sets up an HTTP client and returns it
func InitialiseClient() (*http.Client, error) {
	caCertPool := x509.NewCertPool()
	if config.Config.OIDC.CACert != "" {
		caCert, err := os.ReadFile(config.Config.OIDC.CACert)
		if err != nil {
			log.Errorf("Reading certificate file failed: %v", err)

			return nil, err
		}
		log.Debug("Added certificate")
		caCertPool.AppendCertsFromPEM(caCert)
	} else {
		caCertPool = nil // So that default root certs are used
	}
	// Set up HTTP(S) client
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	t.TLSClientConfig = &tls.Config{
		RootCAs: caCertPool,
	}
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: t}

	return client, nil
}

// HTTPNewRequest stores http.NewRequest, which can be substituted in unit tests
var HTTPNewRequest = http.NewRequest

// MakeRequest builds an authenticated HTTP client
// which sends HTTP requests and parses the responses
var MakeRequest = func(method string, url string, headers map[string]string, body []byte) (*http.Response, error) {
	var (
		response *http.Response
		count    int = 0
	)

	// Build HTTP request
	request, err := HTTPNewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	// Set headers
	for k, v := range headers {
		request.Header.Set(k, v)
	}

	// Execute HTTP request
	// retry the request as specified by httpRetry variable
	for count == 0 || (err != nil && count < 3) {
		// In case of an error, response=nil, which can't be closed,
		// so this lint can be ignored because it would cause a nil pointer deref
		// nolint:bodyclose
		response, err = Client.Do(request)
		count++
	}
	if err != nil {
		return nil, err
	}
	// response.Body is closed in the consumer functions
	// this design lowers memory requirements and makes
	// downloading of larger files more smooth

	// Check StatusCode in case an error has happened downstream and not catched by the `err!=nil`
	if response.StatusCode >= 400 {
		err = errors.New(strconv.Itoa(response.StatusCode))

		return nil, err
	}

	return response, err
}
