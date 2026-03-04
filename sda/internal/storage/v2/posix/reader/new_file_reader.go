package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(_ context.Context, location, filePath string) (io.ReadCloser, error) {
	var locationConfigured bool
	for _, endpoint := range reader.configuredEndpoints {
		if endpoint.Path == location {
			locationConfigured = true

			break
		}
	}
	if !locationConfigured {
		return nil, storageerrors.ErrorNoEndpointConfiguredForLocation
	}

	basePath, err := filepath.Abs(location)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base path for location %s: %w", location, err)
	}

	fullFilePath, err := filepath.Abs(filepath.Join(basePath, filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve file path for %s at location %s: %w", filePath, location, err)
	}

	// Ensure the resolved path is within the configured base path to prevent path traversal.
	baseWithSep := basePath + string(os.PathSeparator)
	fullWithSep := fullFilePath + string(os.PathSeparator)
	if !strings.HasPrefix(fullWithSep, baseWithSep) {
		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	if _, err := os.Stat(fullFilePath); errors.Is(err, os.ErrNotExist) {
		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	file, err := os.Open(fullFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s, at location: %s, due to: %v", filePath, location, err)
	}

	return file, nil
}
