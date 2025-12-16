package reader

import (
	"context"
	"errors"
	"io"

	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
)

func (reader *Reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	if reader == nil {
		return nil, storageerrors.ErrorPosixReaderNotInitialized
	}

	r, err := reader.NewFileReader(ctx, location, filePath)
	if err != nil {
		return nil, err
	}

	seeker, ok := r.(io.ReadSeekCloser)
	if !ok {
		return nil, errors.New("unexpected error: could not cast io.ReadCloser to io.ReadSeekCloser")
	}

	return seeker, nil
}
