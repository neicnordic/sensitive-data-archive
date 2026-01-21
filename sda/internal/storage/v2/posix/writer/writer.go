package writer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

type Writer struct {
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
		locationBroker: locationBroker,
	}

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

		usable, err := endpointConf.isUsable(ctx, writer.locationBroker)
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
