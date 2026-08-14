package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type tracedReadSeekCloser struct {
	io.ReadSeekCloser
	span trace.Span
}

func (r *tracedReadSeekCloser) Close() error {
	err := r.ReadSeekCloser.Close()
	r.span.End()

	return err
}

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	_, span := observability.StartSpan(ctx, "storage.posix.reader.NewFileReader",
		attribute.String("location", location),
		attribute.String("filePath", filePath),
	)

	var locationConfigured bool
	for _, endpoint := range reader.configuredEndpoints {
		if endpoint.Path == location {
			locationConfigured = true

			break
		}
	}
	if !locationConfigured {
		span.End()

		return nil, storageerrors.ErrorNoEndpointConfiguredForLocation
	}

	basePath, err := filepath.Abs(location)
	if err != nil {
		span.End()

		return nil, fmt.Errorf("failed to resolve base path for location %s: %w", location, err)
	}

	fullFilePath, err := filepath.Abs(filepath.Join(basePath, filePath))
	if err != nil {
		span.End()

		return nil, fmt.Errorf("failed to resolve file path for %s at location %s: %w", filePath, location, err)
	}

	// Ensure the resolved path is within the configured base path to prevent path traversal.
	baseWithSep := basePath + string(os.PathSeparator)
	fullWithSep := fullFilePath + string(os.PathSeparator)
	if !strings.HasPrefix(fullWithSep, baseWithSep) {
		span.End()

		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	if _, err := os.Stat(fullFilePath); errors.Is(err, os.ErrNotExist) {
		span.End()

		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	file, err := os.Open(fullFilePath)
	if err != nil {
		span.End()

		return nil, fmt.Errorf("failed to open file: %s, at location: %s, due to: %v", filePath, location, err)
	}

	return &tracedReadSeekCloser{
		ReadSeekCloser: file,
		span:           span,
	}, nil
}
