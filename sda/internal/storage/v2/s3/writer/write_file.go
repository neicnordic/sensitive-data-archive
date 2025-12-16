package writer

import (
	"context"
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

	client, err := writer.activeEndpoint.conf.createClient(ctx)
	if err != nil {
		return "", err
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = int64(writer.activeEndpoint.conf.ChunkSizeBytes)
		u.LeavePartsOnError = false
	})

	// TODO error handling quatos, etc, switch bucket
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Body:            fileContent,
		Bucket:          aws.String(writer.activeEndpoint.bucket),
		Key:             aws.String(filePath),
		ContentEncoding: aws.String("application/octet-stream"),
	})
	if err != nil {
		return "", err
	}

	return writer.activeEndpoint.conf.Endpoint + "/" + writer.activeEndpoint.bucket, nil
}
