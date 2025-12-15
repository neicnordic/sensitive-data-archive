package reader

import (
	"context"
	"errors"
	"io"
)

func (reader *Reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	if reader == nil {
		return nil, ErrorNotInitialized
	}

	r, err := reader.NewFileReader(ctx, location, filePath)
	if err != nil {
		return nil, err
	}

	seeker, ok := r.(io.ReadSeekCloser)
	if !ok {
		return nil, errors.New("invalid posixBackend")
	}

	return seeker, nil
}
