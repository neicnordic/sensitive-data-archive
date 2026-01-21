package reader

import (
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
			return nil, fmt.Errorf("%s is not a directory", loc)
		}
	}
	if len(backend.configuredEndpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	return backend, nil
}
