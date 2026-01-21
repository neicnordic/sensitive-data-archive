package writer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// RemoveFile removes an object from a bucket
func (writer *Writer) RemoveFile(ctx context.Context, location, filePath string) error {
	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return err
	}

	client, err := writer.getS3ClientForEndpoint(ctx, endpoint)
	if err != nil {
		return err
	}

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filePath),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object: %s, bucket: %s, endpoint: %s, due to: %v", filePath, bucket, endpoint, err)
	}

	return nil
}
