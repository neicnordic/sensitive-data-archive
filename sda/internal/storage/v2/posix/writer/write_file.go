package writer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

func (writer *Writer) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (location string, err error) {
	// Find first location that is still usable
	// We need lock for whole WriteFile so no delete occurs while writing
	writer.Lock()
	defer writer.Unlock()

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

	// Ensure a directory is created for temporary write files
	if err = os.MkdirAll(filepath.Join(location, "tmp"), 0700); err != nil {
		return "", fmt.Errorf("failed to create tmp directory at location: %s, due to %v", location, err)
	}

	var tempFile *os.File
	tempFile, err = os.CreateTemp(filepath.Join(location, "tmp"), filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for writing due to: %v", err)
	}
	defer func() {
		// If we did not have any error with writing / renaming, etc no need to clean
		if err == nil {
			return
		}
		// As we might have created empty directories but then encountered error, we need to ensure we do not leave any empty directories since we failed to write the file
		if noEmptyParentsErr := writer.ensureNoEmptyParentDirectories(location, filePath); noEmptyParentsErr != nil {
			log.Errorf("failed to ensure no empty parent directories exist due to: %v", noEmptyParentsErr)
		}
		if osRemoveErr := os.Remove(tempFile.Name()); err != nil && !errors.Is(osRemoveErr, os.ErrNotExist) {
			log.Errorf("failed to remove temp file due to: %v after write failed", osRemoveErr)
		}
	}()

	if _, err = io.Copy(tempFile, fileContent); err != nil {
		_ = tempFile.Close()

		return "", fmt.Errorf("failed to write to file: %s at location: %s, due to: %v", tempFile.Name(), location, err)
	}

	if err = tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close write file: %s at location: %s, due to: %v", tempFile.Name(), location, err)
	}

	// Ensure any parent directories to file exists
	parentDirectories := filepath.Dir(filePath)
	if err = os.MkdirAll(filepath.Join(location, parentDirectories), 0700); err != nil {
		return "", fmt.Errorf("failed to ensure parent directories: %s parentDirectories exists at location: %s, due to: %v", parentDirectories, location, err)
	}

	if err = os.Rename(tempFile.Name(), filepath.Join(location, filePath)); err != nil {
		return "", fmt.Errorf("failed to rename temporary file: %s to %s at location: %s, due to: %v", tempFile.Name(), filePath, location, err)
	}

	return location, nil
}
