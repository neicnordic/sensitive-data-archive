package reader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
)

func (reader *Reader) FindFile(_ context.Context, filePath string) (string, error) {
	for _, endpointConf := range reader.configuredEndpoints {
		_, err := os.Stat(filepath.Join(endpointConf.Path, filePath))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}

			return "", fmt.Errorf("failed to stat file: %s, at location: %s, due to: %v", filePath, endpointConf.Path, err)
		}

		return endpointConf.Path, nil
	}

	return "", storageerrors.ErrorFileNotFoundInLocation
}
