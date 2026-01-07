package storage

import (
	"context"
	"errors"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/broker"
	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
	posixwriter "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/posix/writer"
	s3writer "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/s3/writer"
)

// Writer defines methods to write or delete files from a backend
type Writer interface {
	RemoveFile(ctx context.Context, location, filePath string) error
	// WriteFile will write the file to the active location(returned as location)
	WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (location string, err error)
}

var ErrorNoValidWriter = errors.New("no valid writer configured")

type writer struct {
	posixWriter Writer
	s3Writer    Writer
}

func NewWriter(ctx context.Context, backendName string, locationBroker broker.LocationBroker) (Writer, error) {
	w := &writer{}

	var err error
	w.s3Writer, err = s3writer.NewWriter(ctx, backendName, locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}
	w.posixWriter, err = posixwriter.NewWriter(backendName, locationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidLocations) {
		return nil, err
	}

	if w.s3Writer != nil && w.posixWriter != nil {
		return nil, errors.New("s3 writer and posix writer cannot be used at the same time")
	}
	if w.s3Writer == nil && w.posixWriter == nil {
		return nil, ErrorNoValidWriter
	}

	return w, nil
}

func (w *writer) RemoveFile(ctx context.Context, location, filePath string) error {
	switch {
	case w.s3Writer != nil:
		return w.s3Writer.RemoveFile(ctx, location, filePath)
	case w.posixWriter != nil:
		return w.posixWriter.RemoveFile(ctx, location, filePath)
	}
	return ErrorNoValidWriter
}

func (w *writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	switch {
	case w.s3Writer != nil:
		return w.posixWriter.WriteFile(ctx, filePath, fileContent)
	case w.posixWriter != nil:
		return w.posixWriter.WriteFile(ctx, filePath, fileContent)
	}
	return "", ErrorNoValidWriter
}
