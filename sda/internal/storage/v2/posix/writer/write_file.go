package writer

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

func (writer *Writer) WriteFile(_ context.Context, filePath string, fileContent io.Reader) (string, error) {
	if writer == nil {
		return "", ErrorNotInitialized
	}

	// TODO find valid location
	location := writer.locations[0]

	file, err := os.OpenFile(filepath.Join(location, filePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, fileContent); err != nil {
		return "", err
	}

	return location, nil
}
