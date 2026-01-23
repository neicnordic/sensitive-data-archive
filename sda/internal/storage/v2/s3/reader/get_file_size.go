// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

// GetFileSize returns the size of a specific object
func (reader *Reader) GetFileSize(ctx context.Context, location, filePath string) (int64, error) {
	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return 0, err
	}

	client, err := reader.getS3ClientForEndpoint(ctx, endpoint)
	if err != nil {
		return 0, err
	}

	size, err := reader.getFileSize(ctx, client, bucket, filePath)
	if err != nil {
		return 0, err
	}

	return size, nil
}
func (reader *Reader) getFileSize(ctx context.Context, client *s3.Client, bucket, filePath string) (int64, error) {
	r, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filePath),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return 0, storageerrors.ErrorFileNotFoundInLocation
		}

		return 0, fmt.Errorf("failed to head object: %s, bucket: %s, due to: %v", filePath, bucket, err)
	}

	return *r.ContentLength, nil
}
