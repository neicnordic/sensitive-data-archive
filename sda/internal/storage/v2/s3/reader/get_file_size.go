// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

// GetFileSize returns the size of a specific object
func (reader *Reader) GetFileSize(ctx context.Context, location, filePath string) (int64, error) {
	if reader == nil {
		return 0, storageerrors.ErrorS3ReaderNotInitialized
	}

	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return 0, err
	}

	client, err := reader.createClient(ctx, endpoint)
	if err != nil {
		return 0, err
	}

	return reader.getFileSize(ctx, client, bucket, filePath)
}
func (reader *Reader) getFileSize(ctx context.Context, client *s3.Client, bucket, filePath string) (int64, error) {
	r, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &bucket,
		Key:    &filePath,
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return 0, storageerrors.ErrorFileNotFoundInLocation
		}
		log.Errorf("failed to get object from backend: %v", err)

		return 0, err
	}

	return *r.ContentLength, nil
}
