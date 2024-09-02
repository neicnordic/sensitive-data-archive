package main

import (
	"fmt"
	"net/http"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	log "github.com/sirupsen/logrus"
)

// CheckHealth checks and tries to repair the connections to MQ, DB and S3
func (p *Proxy) CheckHealth(w http.ResponseWriter, _ *http.Request) {

	// try to connect to mq, check connection and channel
	var err error
	if p.messenger == nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}
	if p.messenger.IsConnClosed() {
		log.Warning("connection is closed, reconnecting...")
		p.messenger, err = broker.NewMQ(p.messenger.Conf)
		if err != nil {
			log.Warning(err)
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
	}

	if p.messenger.Channel.IsClosed() {
		log.Warning("channel is closed, recreating...")
		err := p.messenger.CreateNewChannel()
		if err != nil {
			log.Warning(err)
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
	}
	// Ping database, reconnect if there was a connection problem
	err = p.database.DB.Ping()
	if err != nil {
		log.Errorf("Database connection problem: %v", err)
		err = p.database.Connect()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
	}

	// Check that s3 backend responds
	s3URL := p.getS3ReadyPath()
	err = p.httpsGetCheck(s3URL)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.WriteHeader(http.StatusOK)
}

// httpsGetCheck sends a request to the S3 backend and makes sure it is healthy
func (p *Proxy) httpsGetCheck(url string) error {

	resp, e := p.client.Get(url)
	if e != nil {
		return e
	}
	_ = resp.Body.Close() // ignoring error
	if resp.StatusCode != 200 {
		return fmt.Errorf("returned status %d", resp.StatusCode)
	}

	return nil
}

func (p *Proxy) getS3ReadyPath() string {
	s3URL := p.s3.URL
	if p.s3.Port != 0 {
		s3URL = fmt.Sprintf("%s:%d", s3URL, p.s3.Port)
	}
	if p.s3.Readypath != "" {
		s3URL += p.s3.Readypath
	}

	return s3URL
}
