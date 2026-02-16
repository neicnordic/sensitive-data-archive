package reader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

// GetFileSize returns the size of a specific object
func (reader *Reader) GetFileSize(_ context.Context, location, filePath string) (int64, error) {
	var locationConfigured bool
	for _, endpoint := range reader.configuredEndpoints {
		if endpoint.Path == location {
			locationConfigured = true

			break
		}
	}
	if !locationConfigured {
		return 0, storageerrors.ErrorNoEndpointConfiguredForLocation
	}

	basePath, err := filepath.Abs(location)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve base path for location %s: %w", location, err)
	}

	fullFilePath, err := filepath.Abs(filepath.Join(basePath, filePath))
	if err != nil {
		return 0, fmt.Errorf("failed to resolve file path for %s at location %s: %w", filePath, location, err)
	}

	// Ensure the resolved path is within the configured base path to prevent path traversal.
	baseWithSep := basePath + string(os.PathSeparator)
	fullWithSep := fullFilePath + string(os.PathSeparator)
	if !strings.HasPrefix(fullWithSep, baseWithSep) {
		return 0, storageerrors.ErrorFileNotFoundInLocation
	}

	stat, err := os.Stat(fullFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, storageerrors.ErrorFileNotFoundInLocation
		}

		return 0, fmt.Errorf("failed to stat file: %s, at location: %s, due to: %v", filePath, location, err)
	}

	return stat.Size(), nil
}
