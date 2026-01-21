// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
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
		client, err := e.getS3Client(ctx)
		if err != nil {
			return nil, err
		}
		// Use list buckets to verify if client valid
		_, err = client.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to call S3 client at endpoint: %s, due to: %v", e.Endpoint, err)
		}
	}
	if len(backend.endpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	return backend, nil
}

func (reader *Reader) getS3ClientForEndpoint(ctx context.Context, endpoint string) (*s3.Client, error) {
	for _, e := range reader.endpoints {
		if e.Endpoint != endpoint {
			continue
		}
		client, err := e.getS3Client(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 client to endpoint: %s, due to %v", endpoint, err)
		}

		return client, nil
	}

	return nil, storageerrors.ErrorNoEndpointConfiguredForLocation
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
