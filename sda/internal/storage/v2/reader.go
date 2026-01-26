package storage

import (
	"context"
	"errors"
	"io"
	"strings"

	posixreader "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/posix/reader"
	s3reader "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/s3/reader"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

// Reader defines methods to read files from a backend
type Reader interface {
	// NewFileReader will open a reader of the file at the file path in the specified location
	NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error)
	// NewFileReadSeeker will open a read seeker of the file at the file path in the specified location
	NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error)
	// FindFile will look through all configured storages for the specified file path and return the first location in which it was found
	FindFile(ctx context.Context, filePath string) (string, error)
	// GetFileSize will return the size of the file specified by the file path and location
	GetFileSize(ctx context.Context, location, filePath string) (int64, error)
}

type reader struct {
	posixReader Reader
	s3Reader    Reader
}

func NewReader(ctx context.Context, backendName string) (Reader, error) {
	r := &reader{}

	s3Reader, err := s3reader.NewReader(ctx, backendName)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}
	posixReader, err := posixreader.NewReader(backendName)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}

	if s3Reader == nil && posixReader == nil {
		return nil, storageerrors.ErrorNoValidReader
	}
	if posixReader != nil {
		r.posixReader = posixReader
	}
	if s3Reader != nil {
		r.s3Reader = s3Reader
	}

	return r, nil
}

func (r *reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	if strings.HasPrefix(location, "/") && r.posixReader != nil {
		return r.posixReader.NewFileReader(ctx, location, filePath)
	}

	if !strings.HasPrefix(location, "/") && r.s3Reader != nil {
		return r.s3Reader.NewFileReader(ctx, location, filePath)
	}

	return nil, storageerrors.ErrorNoValidReader
}

func (r *reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	if strings.HasPrefix(location, "/") && r.posixReader != nil {
		return r.posixReader.NewFileReadSeeker(ctx, location, filePath)
	}

	if !strings.HasPrefix(location, "/") && r.s3Reader != nil {
		return r.s3Reader.NewFileReadSeeker(ctx, location, filePath)
	}

	return nil, storageerrors.ErrorNoValidReader
}

func (r *reader) GetFileSize(ctx context.Context, location, filePath string) (int64, error) {
	if strings.HasPrefix(location, "/") && r.posixReader != nil {
		return r.posixReader.GetFileSize(ctx, location, filePath)
	}

	if !strings.HasPrefix(location, "/") && r.s3Reader != nil {
		return r.s3Reader.GetFileSize(ctx, location, filePath)
	}

	return 0, storageerrors.ErrorNoValidReader
}

func (r *reader) FindFile(ctx context.Context, filePath string) (string, error) {
	if r.s3Reader != nil {
		loc, err := r.s3Reader.FindFile(ctx, filePath)
		if err != nil && !errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
			return "", err
		}
		if loc != "" {
			return loc, nil
		}
	}
	if r.posixReader != nil {
		loc, err := r.posixReader.FindFile(ctx, filePath)
		if err != nil && !errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
			return "", err
		}
		if loc != "" {
			return loc, nil
		}
	}

	return "", storageerrors.ErrorFileNotFoundInLocation
}
