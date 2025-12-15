// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"os"
	"path/filepath"
)

// GetFileSize returns the size of a specific object
func (reader *Reader) GetFileSize(_ context.Context, location, filePath string) (int64, error) {
	if reader == nil {
		return 0, ErrorNotInitialized
	}

	stat, err := os.Stat(filepath.Join(location, filePath))
	if err != nil {
		return 0, err
	}

	return stat.Size(), nil
}
