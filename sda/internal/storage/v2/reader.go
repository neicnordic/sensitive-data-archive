package storage

import (
	"context"
	"io"
	"strings"

	posixreader "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/posix/reader"
	s3reader "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/s3/reader"
)

// Reader defines methods to read files from a backend
type Reader interface {
	NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error)
	NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error)
	GetFileSize(ctx context.Context, location, filePath string) (int64, error)
}

type reader struct {
	posixReader Reader
	s3Reader    Reader
}

func NewReader(ctx context.Context, backendName string) (Reader, error) {
	r := &reader{}

	var err error
	r.s3Reader, err = s3reader.NewReader(ctx, backendName)
	if err != nil {
		return nil, err
	}
	r.posixReader, err = posixreader.NewReader(backendName)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	if strings.HasPrefix(location, "/") {
		return r.posixReader.NewFileReader(ctx, location, filePath)
	}

	return r.s3Reader.NewFileReader(ctx, location, filePath)
}

func (r *reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	if strings.HasPrefix(location, "/") {
		return r.posixReader.NewFileReadSeeker(ctx, location, filePath)
	}

	return r.s3Reader.NewFileReadSeeker(ctx, location, filePath)
}

func (r *reader) GetFileSize(ctx context.Context, location, filePath string) (int64, error) {
	if strings.HasPrefix(location, "/") {
		return r.posixReader.GetFileSize(ctx, location, filePath)
	}

	return r.s3Reader.GetFileSize(ctx, location, filePath)
}
