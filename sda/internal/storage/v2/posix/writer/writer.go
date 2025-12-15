package writer

import (
	"errors"
	"fmt"
	"os"
)

var ErrorNoValidLocations = errors.New("no valid locations")
var ErrorNotInitialized = errors.New("posix writer has not been initialized")

type Writer struct {
	locations []string
}

func NewWriter(backendName string) (*Writer, error) {
	endPoints, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	backend := &Writer{
		locations: endPoints,
	}
	// Verify locations
	for _, loc := range backend.locations {
		fileInfo, err := os.Stat(loc)

		if err != nil {
			return nil, err
		}

		if !fileInfo.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", loc)
		}
		// TODO fix active location, eg evaluate file count / size, etc
	}

	if len(backend.locations) == 0 {
		return nil, ErrorNoValidLocations
	}

	return backend, nil
}
