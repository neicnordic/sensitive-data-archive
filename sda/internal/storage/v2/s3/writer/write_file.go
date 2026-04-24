package writer

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	// Find endpoint / bucket that is to be used for writing
	writer.Lock()
	activeBucket, err := writer.activeEndpoint.findActiveBucket(ctx, writer.backendName, writer.locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoFreeBucket) {
		writer.Unlock()

		return "", err
	}
	// Current active endpoint no longer has any free buckets, roll over to next endpoint
	if activeBucket == "" {
		for _, endpointConf := range writer.configuredEndpoints {
			// We dont need to evaluate the currently active bucket as we know it doesnt have any active buckets now
			if endpointConf.Endpoint == writer.activeEndpoint.Endpoint {
				continue
			}

			activeBucket, err = endpointConf.findActiveBucket(ctx, writer.backendName, writer.locationBroker)
			if err != nil {
				if errors.Is(err, storageerrors.ErrorNoFreeBucket) {
					continue
				}
				writer.Unlock()

				return "", err
			}
			writer.activeEndpoint = endpointConf

			break
		}
	}
	writer.Unlock()

	client, err := writer.activeEndpoint.getS3Client(ctx)
	if err != nil {
		return "", err
	}

	// Classical multipart via the stable s3/manager; the preview s3/transfermanager
	// emits STREAMING-UNSIGNED-PAYLOAD-TRAILER which older Ceph RGW rejects.
	// RequestChecksumCalculation must also be set on the Uploader itself — the
	// multipart path ignores the s3.Client setting and otherwise forces CRC32,
	// which re-triggers the streaming trailer middleware.
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		// Type conversion safe as chunkSizeBytes is checked to be between 5mb and 1gb (in bytes)
		//nolint:gosec // disable G115
		u.PartSize = int64(writer.activeEndpoint.chunkSizeBytes)
		u.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})

	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Body:   fileContent,
		Bucket: aws.String(activeBucket),
		Key:    aws.String(filePath),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload object: %s, bucket: %s, endpoint: %s, due to: %v", filePath, activeBucket, writer.activeEndpoint.Endpoint, err)
	}

	return writer.activeEndpoint.Endpoint + "/" + activeBucket, nil
}
