package writer

import (
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	if writer == nil {
		return "", storageerrors.ErrorS3WriterNotInitialized
	}

	// TODO do we need to check this at each write?

	// TODO locking while finding active bucket????
	activeBucket, err := writer.activeEndpoint.findActiveBucket(ctx, writer.locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoFreeBucket) {
		return "", err
	}
	// Current active endpoint no longer has any free buckets, roll over to next endpoint
	if activeBucket == "" {
		for _, endpointConf := range writer.configuredEndpoints {
			activeBucket, err = endpointConf.findActiveBucket(ctx, writer.locationBroker)
			if err != nil {
				if errors.Is(err, storageerrors.ErrorNoFreeBucket) {
					continue
				}

				return "", nil
			}
			writer.activeEndpoint = endpointConf

			break
		}
	}

	client, err := writer.activeEndpoint.createClient(ctx)
	if err != nil {
		return "", err
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		// Type conversation safe as ChunkSizeBytes checked to be max math.MaxInt
		//nolint:gosec // disable G115
		u.PartSize = int64(writer.activeEndpoint.ChunkSizeBytes)
		u.LeavePartsOnError = false
	})

	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Body:            fileContent,
		Bucket:          aws.String(activeBucket),
		Key:             aws.String(filePath),
		ContentEncoding: aws.String("application/octet-stream"),
	})
	if err != nil {
		return "", err
	}

	return writer.activeEndpoint.Endpoint + "/" + activeBucket, nil
}
