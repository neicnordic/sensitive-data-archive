package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	fullFilePath := filepath.Join(location, filePath)

	if _, err := os.Stat(fullFilePath); errors.Is(err, os.ErrNotExist) {
		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	file, err := os.Open(fullFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s, at location: %s, due to: %v", filePath, location, err)
	}

	return file, nil
}
