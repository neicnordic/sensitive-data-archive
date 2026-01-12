package writer

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// RemoveFile removes an object from a bucket
func (writer *Writer) RemoveFile(ctx context.Context, location, filePath string) error {
	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return err
	}

	client, err := writer.createClient(ctx, endpoint)
	if err != nil {
		return err
	}

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filePath),
	})
	if err != nil {
		return err
	}

	return nil
}
