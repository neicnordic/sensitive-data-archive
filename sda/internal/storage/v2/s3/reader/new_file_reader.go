package reader

import (
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
	log "github.com/sirupsen/logrus"
)

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	if reader == nil {
		return nil, storageerrors.ErrorS3ReaderNotInitialized
	}

	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return nil, err
	}

	client, err := reader.createClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	r, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filePath),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return nil, storageerrors.ErrorFileNotFoundInLocation
		}
		log.Errorf("failed to get object from backend: %v", err)

		return nil, err
	}

	return r.Body, nil
}
