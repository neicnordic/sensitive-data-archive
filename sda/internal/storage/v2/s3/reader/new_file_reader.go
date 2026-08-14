package reader

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type tracedReadCloser struct {
	io.ReadCloser
	span trace.Span
}

func (r *tracedReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.span.End()

	return err
}

func (reader *Reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	ctx, span := observability.StartSpan(ctx, "storage.s3.reader.NewFileReader",
		attribute.String("location", location),
		attribute.String("filePath", filePath),
	)

	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		span.End()

		return nil, err
	}

	client, _, err := reader.getS3ClientForEndpoint(ctx, endpoint)
	if err != nil {
		span.End()

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
		span.End()

		return nil, fmt.Errorf("failed to get object: %s, bucket: %s, endpoint: %s, due to: %v", filePath, bucket, endpoint, err)
	}

	return &tracedReadCloser{
		ReadCloser: r.Body,
		span:       span,
	}, nil
}
