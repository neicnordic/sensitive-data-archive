package writer

import (
	"context"
	"os"
	"path/filepath"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

func (writer *Writer) RemoveFile(_ context.Context, location, filePath string) error {
	if writer == nil {
		return storageerrors.ErrorPosixWriterNotInitialized
	}
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

	return os.Remove(filepath.Join(location, filePath))
}
