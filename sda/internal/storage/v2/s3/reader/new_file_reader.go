package reader

import (
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

func (reader *Reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return nil, err
	}

	client, err := reader.getS3ClientForEndpoint(ctx, endpoint)
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
