package writer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"

	log "github.com/sirupsen/logrus"
)

type Writer struct {
	configuredEndpoints []*endpointConfig
	activeEndpoint      *endpointConfig

	locationBroker locationbroker.LocationBroker

	sync.Mutex
}

// NewWriter initiates a storage backend
func NewWriter(ctx context.Context, backendName string, locationBroker locationbroker.LocationBroker) (*Writer, error) {
	endPointConf, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	if locationBroker == nil {
		return nil, errors.New("locationBroker is required")
	}

	writer := &Writer{
		locationBroker: locationBroker,
	}

	// Verify endpointConfig connections
	for _, e := range endPointConf {
		_, err := e.findActiveBucket(ctx, writer.locationBroker)
		if err != nil {
			if errors.Is(err, storageerrors.ErrorNoFreeBucket) {
				log.Warningf("s3: %s has no available bucket", e.Endpoint)
				writer.configuredEndpoints = append(writer.configuredEndpoints, e)

				continue
			}

			return nil, err
		}
		writer.configuredEndpoints = append(writer.configuredEndpoints, e)
		// Set first active endpoint as current
		if writer.activeEndpoint == nil {
			writer.activeEndpoint = e
		}
	}

	if len(writer.configuredEndpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	return writer, nil
}
func (writer *Writer) getS3ClientForEndpoint(ctx context.Context, endpoint string) (*s3.Client, error) {
	for _, e := range writer.configuredEndpoints {
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
