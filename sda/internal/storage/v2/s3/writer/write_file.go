package writer

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"go.opentelemetry.io/otel/attribute"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	ctx, span := observability.StartSpan(ctx, "storage.s3.writer.WriteFile",
		attribute.String("filePath", filePath),
	)
	defer span.End()

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

	span.SetAttributes(attribute.String("activeBucket", writer.activeEndpoint.Endpoint))
	span.SetAttributes(attribute.String("activeEndpoint", activeBucket))

	writer.Unlock()

	// No endpoint had a free bucket, so there is nowhere to write. Bail out
	// before the upload runs against an empty bucket name.
	if activeBucket == "" {
		return "", storageerrors.ErrorNoFreeBucket
	}

	client, err := writer.activeEndpoint.getS3Client(ctx)
	if err != nil {
		return "", err
	}

	uploader := transfermanager.New(client, func(u *transfermanager.Options) {
		// Type conversation safe as chunkSizeBytes checked to be between 5mb and 1gb (in bytes)
		//nolint:gosec // disable G115
		u.PartSizeBytes = int64(writer.activeEndpoint.chunkSizeBytes)
	})

	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Body:   fileContent,
		Bucket: aws.String(activeBucket),
		Key:    aws.String(filePath),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload object: %s, bucket: %s, endpoint: %s, due to: %v", filePath, activeBucket, writer.activeEndpoint.Endpoint, err)
	}

	return writer.activeEndpoint.Endpoint + "/" + activeBucket, nil
}
