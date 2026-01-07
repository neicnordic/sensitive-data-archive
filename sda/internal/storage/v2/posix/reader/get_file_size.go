package reader

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
)

// GetFileSize returns the size of a specific object
func (reader *Reader) GetFileSize(_ context.Context, location, filePath string) (int64, error) {
	if reader == nil {
		return 0, storageerrors.ErrorPosixReaderNotInitialized
	}

	stat, err := os.Stat(filepath.Join(location, filePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, storageerrors.ErrorFileNotFoundInLocation
		}
		return 0, err
	}

	return stat.Size(), nil
}
