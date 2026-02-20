package writer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"

	log "github.com/sirupsen/logrus"
)

type Writer struct {
	backendName         string
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
		backendName:    backendName,
		locationBroker: locationBroker,
	}
	writer.locationBroker.RegisterSizeAndCountFinderFunc(backendName, func(location string) bool {
		return !strings.HasPrefix(location, "/")
	}, findSizeAndObjectCountOfLocation(endPointConf))

	// Verify endpointConfig connections
	for _, e := range endPointConf {
		_, err := e.findActiveBucket(ctx, backendName, writer.locationBroker)
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
func getS3ClientForEndpoint(ctx context.Context, configuredEndpoints []*endpointConfig, endpoint string) (*s3.Client, error) {
	for _, e := range configuredEndpoints {
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

// findSizeAndObjectCountOfLocation find the total size and total amount of objects in a s3 bucket if we do not store
// this information in the database
func findSizeAndObjectCountOfLocation(configuredEndpoints []*endpointConfig) func(ctx context.Context, location string) (uint64, uint64, error) {
	return func(ctx context.Context, location string) (uint64, uint64, error) {
		endpoint, bucket, err := parseLocation(location)
		if err != nil {
			return 0, 0, err
		}

		s3Client, err := getS3ClientForEndpoint(ctx, configuredEndpoints, endpoint)
		if err != nil {
			return 0, 0, err
		}

		var totalSize uint64
		var totalObjects uint64

		paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		})

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return 0, 0, err
			}

			for _, obj := range page.Contents {
				totalObjects++
				if obj.Size != nil && *obj.Size > 0 {
					totalSize += uint64(*obj.Size) // #nosec G115 -- *obj.Size has been checked to be bigger than 0
				}
			}
		}

		return totalSize, totalObjects, nil
	}
}
