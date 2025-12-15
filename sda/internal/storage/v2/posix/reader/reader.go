package reader

import (
	"errors"
	"fmt"
	"os"
)

var ErrorNotInitialized = errors.New("posix reader has not been initialized")

type Reader struct {
	locations []string
}

func NewReader(backendName string) (*Reader, error) {
	endPoints, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	backend := &Reader{
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

	}
	return backend, nil
}
