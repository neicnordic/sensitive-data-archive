package storage

import (
	"context"
	"errors"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	posixwriter "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/posix/writer"
	s3writer "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/s3/writer"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

// Writer defines methods to write or delete files from a backend
type Writer interface {
	RemoveFile(ctx context.Context, location, filePath string) error
	// WriteFile will write the file to the active location(returned as location)
	WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (location string, err error)
}

type writer struct {
	writer Writer
}

func NewWriter(ctx context.Context, backendName string, locationBroker locationbroker.LocationBroker) (Writer, error) {
	w := &writer{}

	var err error
	s3Writer, err := s3writer.NewWriter(ctx, backendName, locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}
	posixWriter, err := posixwriter.NewWriter(ctx, backendName, locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}

	if s3Writer != nil && posixWriter != nil {
		return nil, storageerrors.ErrorMultipleWritersNotSupported
	}
	switch {
	case s3Writer != nil:
		w.writer = s3Writer
	case posixWriter != nil:
		w.writer = posixWriter
	default:
		return nil, storageerrors.ErrorNoValidWriter
	}

	return w, nil
}

func (w *writer) RemoveFile(ctx context.Context, location, filePath string) error {
	return w.writer.RemoveFile(ctx, location, filePath)
}

func (w *writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	return w.writer.WriteFile(ctx, filePath, fileContent)
}
