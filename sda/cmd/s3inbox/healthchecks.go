package main

import (
	"fmt"
	"net/http"
	"net/url"
	"path"

	log "github.com/sirupsen/logrus"
)

// CheckHealth does a health check of the connections to the DB, S3, and MQ
func (p *proxy) CheckHealth(w http.ResponseWriter, r *http.Request) {
	// try to connect to mq, check connection and channel
	var err error
	if p.broker == nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}
	if !p.broker.Alive() {
		log.Error("broker connection not alive")
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}

	// Ping database, reconnect if there was a connection problem
	err = p.database.Ping(r.Context())
	if err != nil {
		log.Errorf("Database connection problem: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}

	// Check that s3 backend responds
	s3url, err := p.getS3ReadyPath()
	if err != nil {
		log.Errorf("Incorrect S3 health url: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}
	err = p.httpsGetCheck(s3url)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}
	w.WriteHeader(http.StatusOK)
}

// httpsGetCheck sends a request to the S3 backend and makes sure it is healthy
func (p *proxy) httpsGetCheck(uri string) error {
	resp, e := p.client.Get(uri) // #nosec G704 uri originates from configuration
	if e != nil {
		return e
	}
	_ = resp.Body.Close() // ignoring error
	if resp.StatusCode != 200 {
		return fmt.Errorf("returned status %d", resp.StatusCode)
	}

	return nil
}

func (p *proxy) getS3ReadyPath() (string, error) {
	s3URL, err := url.Parse(p.s3Conf.endpoint)
	if err != nil {
		return "", err
	}
	if p.s3Conf.readyPath != "" {
		s3URL.Path = path.Join(s3URL.Path, p.s3Conf.readyPath)
	}

	return s3URL.String(), nil
}
