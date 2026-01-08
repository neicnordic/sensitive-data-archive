// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"

	log "github.com/sirupsen/logrus"
)

type Reader struct {
	endpoints []*endpointConfig
}

func NewReader(ctx context.Context, backendName string) (*Reader, error) {
	endPoints, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	backend := &Reader{
		endpoints: endPoints,
	}
	// Verify endpoint connections
	for _, e := range backend.endpoints {
		client, err := e.createClient(ctx)
		if err != nil {
			log.Errorf("failed to create S3 client: %v to endpoint: %s", err, e.Endpoint)

			return nil, err
		}
		// Use list buckets to verify if client valid
		_, err = client.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			log.Errorf("failed to call S3 client: %v to endpoint: %s", err, e.Endpoint)

			return nil, err
		}
	}
	if len(backend.endpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	return backend, nil
}

func (reader *Reader) createClient(ctx context.Context, endpoint string) (*s3.Client, error) {
	for _, e := range reader.endpoints {
		if e.Endpoint != endpoint {
			continue
		}
		client, err := e.createClient(ctx)
		if err != nil {
			log.Errorf("failed to create S3 client: %v to endpoint: %s", err, endpoint)

			return nil, err
		}

		return client, nil
	}

	log.Errorf("no valid reader endpoints configured for endpoint: %s", endpoint)

	return nil, fmt.Errorf("no valid reader endpoints configured for endpoint: %s", endpoint)
}
func (endpointConf *endpointConfig) createClient(ctx context.Context) (*s3.Client, error) {
	s3cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(endpointConf.AccessKey, endpointConf.SecretKey, "")),
		config.WithHTTPClient(&http.Client{Transport: endpointConf.transportConfigS3()}),
	)
	if err != nil {
		return nil, err
	}

	endpoint := endpointConf.Endpoint
	switch {
	case !strings.HasPrefix(endpoint, "http") && endpointConf.DisableHTTPS:
		endpoint = "http://" + endpoint
	case !strings.HasPrefix(endpoint, "https") && !endpointConf.DisableHTTPS:
		endpoint = "https://" + endpoint
	default:
	}

	return s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.EndpointOptions.DisableHTTPS = endpointConf.DisableHTTPS
			o.Region = endpointConf.Region
		},
	), nil
}

// transportConfigS3 is a helper method to setup TLS for the S3 client.
func (endpointConf *endpointConfig) transportConfigS3() http.RoundTripper {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if endpointConf.CACert != "" {
		cacert, e := os.ReadFile(endpointConf.CACert) // #nosec this file comes from our config
		if e != nil {
			log.Fatalf("failed to append %q to RootCAs: %v", cacert, e) // nolint # FIXME Fatal should only be called from main
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	var trConfig http.RoundTripper = &http.Transport{
		TLSClientConfig:   cfg,
		ForceAttemptHTTP2: true}

	return trConfig
}

// parseLocation attempts to parse a location to a s3 endpoint, and a bucket
// expected format of location is "${ENDPOINT}/${BUCKET}
func parseLocation(location string) (string, string, error) {
	locAsURL, err := url.Parse(location)
	if err != nil {
		return "", "", storageerrors.ErrorInvalidLocation
	}

	endpoint := strings.TrimSuffix(location, locAsURL.RequestURI())
	if endpoint == "" {
		return "", "", storageerrors.ErrorInvalidLocation
	}
	bucketName := strings.TrimPrefix(locAsURL.RequestURI(), "/")
	if bucketName == "" {
		return "", "", storageerrors.ErrorInvalidLocation
	}

	return endpoint, bucketName, nil
}
