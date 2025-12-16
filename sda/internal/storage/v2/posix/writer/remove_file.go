package writer

import (
	"context"
	"os"
	"path/filepath"

	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
)

func (writer *Writer) RemoveFile(_ context.Context, location, filePath string) error {
	if writer == nil {
		return storageerrors.ErrorPosixWriterNotInitialized
	}

	return os.Remove(filepath.Join(location, filePath))
}
