package writer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

func (writer *Writer) RemoveFile(_ context.Context, location, filePath string) error {
	// We need lock for whole RemoveFile so no write occurs while deleting
	writer.Lock()
	defer writer.Unlock()

	var locationConfigured bool
	for _, endpoint := range writer.configuredEndpoints {
		if endpoint.Path == location {
			locationConfigured = true

			break
		}
	}
	if !locationConfigured {
		return storageerrors.ErrorNoEndpointConfiguredForLocation
	}

	if err := os.Remove(filepath.Join(location, filePath)); err != nil {
		return fmt.Errorf("failed to remove file: %s, from location: %s, due to: %v", filePath, location, err)
	}

	return writer.ensureNoEmptyParentDirectories(location, filePath)
}

func (writer *Writer) ensureNoEmptyParentDirectories(location, filePath string) error {
	// Check if any parent directories are empty and delete if empty
	fileParentDirectories := filepath.Dir(filePath)

	for range strings.Split(fileParentDirectories, string(os.PathSeparator)) {
		dirEntries, err := os.ReadDir(filepath.Join(location, fileParentDirectories))
		if err != nil {
			// Since we already removed the file we just log error and return nil
			log.Errorf("failed to get dirctory entires: %s at location: %s, due to: %v", fileParentDirectories, location, err)

			return nil
		}
		// Since the this dir isnt empty no point checking its parents
		if len(dirEntries) > 0 {
			return nil
		}
		if err := os.Remove(filepath.Join(location, fileParentDirectories)); err != nil {
			return fmt.Errorf("failed to remove directory: %s which is empty, from location: %s, due to: %v", fileParentDirectories, location, err)
		}

		fileParentDirectories = filepath.Dir(fileParentDirectories)
	}

	return nil
}
