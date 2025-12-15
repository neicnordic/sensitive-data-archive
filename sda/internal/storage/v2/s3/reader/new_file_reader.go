// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"
)

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	if reader == nil {
		return nil, ErrorNotInitialized
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
		log.Errorf("failed to get object from backend: %v", err)

		return nil, err
	}

	return r.Body, nil
}
