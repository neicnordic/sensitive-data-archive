package reader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"go.opentelemetry.io/otel/attribute"
)

func (reader *Reader) FindFile(ctx context.Context, filePath string) (string, error) {
	_, span := observability.StartSpan(ctx, "storage.posix.reader.FindFile",
		attribute.String("filePath", filePath),
	)
	defer span.End()

	for _, endpointConf := range reader.configuredEndpoints {
		basePath, err := filepath.Abs(endpointConf.Path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve base path for location %s: %w", endpointConf.Path, err)
		}

		fullFilePath, err := filepath.Abs(filepath.Join(basePath, filePath))
		if err != nil {
			return "", fmt.Errorf("failed to resolve file path for %s at location %s: %w", filePath, endpointConf.Path, err)
		}

		// Ensure the resolved path is within the configured base path to prevent path traversal.
		baseWithSep := basePath + string(os.PathSeparator)
		fullWithSep := fullFilePath + string(os.PathSeparator)
		if !strings.HasPrefix(fullWithSep, baseWithSep) {
			return "", storageerrors.ErrorFileNotFoundInLocation
		}

		_, err = os.Stat(fullFilePath)
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
