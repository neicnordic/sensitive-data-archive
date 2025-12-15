package writer

import (
	"context"
	"os"
	"path/filepath"
)

func (writer *Writer) RemoveFile(_ context.Context, location, filePath string) error {
	if writer == nil {
		return ErrorNotInitialized
	}

	return os.Remove(filepath.Join(location, filePath))
}
