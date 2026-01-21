package writer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	// Find first location that is still usable
	// We need lock for whole WriteFile so no delete occurs while writing
	writer.Lock()
	defer writer.Unlock()

	var location string
	for {
		if len(writer.activeEndpoints) == 0 {
			return "", storageerrors.ErrorNoValidLocations
		}

		usable, err := writer.activeEndpoints[0].isUsable(ctx, writer.locationBroker)
		if err != nil {
			return "", fmt.Errorf("failed to check if location: %s is usable: %v", writer.activeEndpoints[0].Path, err)
		}
		if usable {
			location = writer.activeEndpoints[0].Path

			break
		}
		writer.activeEndpoints = writer.activeEndpoints[1:]
	}

	// Ensure any parent directories to file exists
	parentDirectories := filepath.Dir(filePath)
	if err := os.MkdirAll(filepath.Join(location, parentDirectories), 0700); err != nil {
		return "", fmt.Errorf("failed to ensure parent directories exists, due to: %v", err)
	}

	file, err := os.OpenFile(filepath.Join(location, filePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %s at location: %s, due to: %v", filePath, location, err)
	}
	if _, err := io.Copy(file, fileContent); err != nil {
		_ = file.Close()

		return "", fmt.Errorf("failed to write to file: %s at location: %s, due to: %v", filePath, location, err)
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close file: %s at location: %s, due to: %v", filePath, location, err)
	}

	return location, nil
}
