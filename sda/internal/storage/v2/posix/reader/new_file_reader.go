// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
)

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(_ context.Context, location, filePath string) (io.ReadCloser, error) {
	if reader == nil {
		return nil, storageerrors.ErrorPosixReaderNotInitialized
	}

	fullFilePath := filepath.Join(location, filePath)

	if _, err := os.Stat(fullFilePath); errors.Is(err, os.ErrNotExist) {
		return nil, storageerrors.ErrorFileNotFoundInLocation
	}

	file, err := os.Open(fullFilePath)
	if err != nil {
		return nil, err
	}

	return file, nil
}
