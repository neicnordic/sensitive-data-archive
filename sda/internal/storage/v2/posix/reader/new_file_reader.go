// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package reader

import (
	"context"
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// NewFileReader returns an io.Reader instance
func (reader *Reader) NewFileReader(_ context.Context, location, filePath string) (io.ReadCloser, error) {
	if reader == nil {
		return nil, ErrorNotInitialized
	}

	file, err := os.Open(filepath.Join(location, filePath))
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return file, nil
}
