package writer

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	// Find first location that is still usable
	// TODO locking while finding active location????
	var location string
	for {
		if len(writer.activeEndpoints) == 0 {
			return "", storageerrors.ErrorNoValidLocations
		}

		usable, err := writer.activeEndpoints[0].isUsable(ctx, writer.locationBroker)
		if err != nil {
			return "", err
		}
		if usable {
			location = writer.activeEndpoints[0].Path

			break
		}
		writer.activeEndpoints = writer.activeEndpoints[1:]
	}

	file, err := os.OpenFile(filepath.Join(location, filePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, fileContent); err != nil {
		return "", err
	}

	return location, nil
}
