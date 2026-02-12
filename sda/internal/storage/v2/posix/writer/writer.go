package writer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

type Writer struct {
	backendName         string
	configuredEndpoints []*endpointConfig
	activeEndpoints     []*endpointConfig
	locationBroker      locationbroker.LocationBroker

	sync.Mutex
}

func NewWriter(ctx context.Context, backendName string, locationBroker locationbroker.LocationBroker) (*Writer, error) {
	endPoints, err := loadConfig(backendName)
	if err != nil {
		return nil, err
	}

	if locationBroker == nil {
		return nil, errors.New("locationBroker is required")
	}

	writer := &Writer{
		backendName:    backendName,
		locationBroker: locationBroker,
	}
	writer.locationBroker.RegisterSizeAndCountFinderFunc(backendName, func(location string) bool {
		return !strings.HasPrefix(location, "/")
	}, findSizeAndObjectCountInDir)

	// Verify locations
	for _, endpointConf := range endPoints {
		fileInfo, err := os.Stat(endpointConf.Path)

		if err != nil {
			return nil, fmt.Errorf("failed to describe path: %s, reason: %v", endpointConf.Path, err)
		}

		if !fileInfo.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", endpointConf.Path)
		}

		writer.configuredEndpoints = append(writer.configuredEndpoints, endpointConf)

		usable, err := endpointConf.isUsable(ctx, backendName, writer.locationBroker)
		if err != nil {
			return nil, err
		}
		if !usable {
			log.Infof("posix path: %s, has reached its max object count or max size", endpointConf.Path)

			continue
		}

		writer.activeEndpoints = append(writer.activeEndpoints, endpointConf)
	}

	if len(writer.activeEndpoints) == 0 {
		return nil, storageerrors.ErrorNoValidLocations
	}

	return writer, nil
}

// findSizeAndObjectCountInDir find the total size and total amount of objects in an directory if we do not store
// this information in the database
func findSizeAndObjectCountInDir(_ context.Context, dir string) (uint64, uint64, error) {
	totalObjects := uint64(0)
	totalSize := uint64(0)

	if err := filepath.Walk(dir, func(_ string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		fileSize := info.Size()
		if fileSize < 0 {
			return fmt.Errorf("file: %s has negative size", info.Name())
		}
		totalSize += uint64(fileSize)
		totalObjects++

		return nil
	}); err != nil {
		return 0, 0, err
	}

	return totalSize, totalObjects, nil
}
