package writer

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/broker"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"

	log "github.com/sirupsen/logrus"
)

type Writer struct {
	configuredEndpoints []*endpointConfig
	activeEndpoint      *endpointConfig

	locationBroker broker.LocationBroker

	sync.RWMutex
}

// NewWriter initiates a storage backend
func NewWriter(ctx context.Context, backendName string, locationBroker broker.LocationBroker) (*Writer, error) {
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
func (writer *Writer) createClient(ctx context.Context, endpoint string) (*s3.Client, error) {
	for _, e := range writer.configuredEndpoints {
		if e.Endpoint != endpoint {
			continue
		}
		client, err := e.createClient(ctx)
		if err != nil {
			log.Errorf("failed to create S3 client: %v to s3: %s", err, endpoint)

			return nil, err
		}

		return client, nil
	}

	return nil, storageerrors.ErrorNoEndpointConfiguredForLocation
}

// parseLocation attempts to parse a location to a s3 endpointConfig, and a bucket
// expected format of location is "${ENDPOINT}/${BUCKET}
func parseLocation(location string) (string, string, error) {
	split := strings.Split(location, "/")
	if len(split) != 2 {
		return "", "", errors.New("invalid location")
	}
	if split[0] == "" {
		return "", "", errors.New("invalid location, empty endpointConfig")
	}
	if split[1] == "" {
		return "", "", errors.New("invalid location, empty bucket")
	}

	return split[0], split[1], nil
}
