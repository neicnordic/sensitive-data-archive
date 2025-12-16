package writer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"

	log "github.com/sirupsen/logrus"
)

type Writer struct {
	validEndpoints []*endpointConfig
	activeEndpoint *validEndpoint
}

type validEndpoint struct {
	conf   *endpointConfig
	bucket string
}

// NewWriter initiates a storage backend
func NewWriter(ctx context.Context, backendName string) (*Writer, error) {
	endPointConf, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	writer := &Writer{}

	// Verify endpointConfig connections
	for _, e := range endPointConf {

		bucket, err := e.findActiveBucket(ctx)
		if err != nil {
			if errors.Is(err, storageerrors.ErrorMaxBucketReached) {
				log.Warningf("s3: %s has reach max bucket quota", e.Endpoint)
				continue
			}
			return nil, err
		}
		// TODO fix active bucket, eg evaluate object count / size, etc
		writer.validEndpoints = append(writer.validEndpoints, e)
		// Set first active endpoint as current
		if writer.activeEndpoint == nil {
			writer.activeEndpoint = &validEndpoint{
				conf:   e,
				bucket: bucket,
			}
		}
	}

	if len(writer.validEndpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}
	return writer, nil
}
func (writer *Writer) createClient(ctx context.Context, endpoint string) (*s3.Client, error) {
	for _, e := range writer.validEndpoints {
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

	log.Errorf("no valid endpoints configured for: %s", endpoint)
	return nil, fmt.Errorf("no valid endpoints configured for: %s", endpoint)
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
