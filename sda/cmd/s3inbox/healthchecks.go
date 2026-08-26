package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	log "github.com/sirupsen/logrus"
)

// CheckHealth does a health check of the connections to the DB, S3, and MQ
func (p *Proxy) CheckHealth(w http.ResponseWriter, r *http.Request) {
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
	err = p.httpsGetCheck(r.Context(), s3url)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}
	w.WriteHeader(http.StatusOK)
}

// httpsGetCheck sends a request to the S3 backend and makes sure it is healthy
func (p *Proxy) httpsGetCheck(ctx context.Context, uri string) error {
	// Use a dedicated context timeout for readiness checks so s3inbox logs the timeout first
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, e := p.client.Do(req)
	if e != nil {
		return fmt.Errorf("S3 backend check failed for %s: %w", uri, e)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("S3 check to %s returned status %d: %s", uri, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	return nil
}

func (p *Proxy) getS3ReadyPath() (string, error) {
	s3URL, err := url.Parse(p.s3Conf.Endpoint)
	if err != nil {
		return "", err
	}
	if p.s3Conf.ReadyPath != "" {
		s3URL.Path = path.Join(s3URL.Path, p.s3Conf.ReadyPath)
	}

	return s3URL.String(), nil
}
