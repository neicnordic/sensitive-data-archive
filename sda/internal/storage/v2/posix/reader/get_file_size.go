package reader

import (
	"context"
	"errors"
	"os"
	"path/filepath"

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

	stat, err := os.Stat(filepath.Join(location, filePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, storageerrors.ErrorFileNotFoundInLocation
		}

		return 0, err
	}

	return stat.Size(), nil
}
