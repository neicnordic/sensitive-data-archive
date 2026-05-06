package reader

import (
	"context"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

type Reader struct {
	configuredEndpoints []*endpointConfig
}

func NewReader(backendName string) (*Reader, error) {
	endPointsConfigurations, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}
	if len(endPointsConfigurations) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	backend := &Reader{
		configuredEndpoints: endPointsConfigurations,
	}
	// Verify configuredEndpoints
	for _, loc := range backend.configuredEndpoints {
		fileInfo, err := os.Stat(loc.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to stat location: %s, due to: %v", loc.Path, err)
		}

		if !fileInfo.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", loc.Path)
		}
	}

	return backend, nil
}

// Ping verifies all configured POSIX paths are accessible directories.
func (r *Reader) Ping(_ context.Context) error {
	for _, e := range r.configuredEndpoints {
		fileInfo, err := os.Stat(e.Path)
		if err != nil {
			return fmt.Errorf("failed to ping POSIX path: %s, due to: %v", e.Path, err)
		}
		if !fileInfo.IsDir() {
			return fmt.Errorf("failed to ping POSIX path: %s is not a directory", e.Path)
		}
	}

	return nil
}
