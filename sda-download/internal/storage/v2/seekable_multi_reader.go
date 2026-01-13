package storage

import (
	"errors"
	"fmt"
	"io"
)

// seekableMultiReader is a helper struct to allow io.MultiReader to be used with a seekable reader
type seekableMultiReader struct {
	readers       []io.Reader
	sizes         []int64
	currentOffset int64
	totalSize     int64
}

// SeekableMultiReader constructs a multireader that supports seeking. Requires
// all passed readers to be seekable
func SeekableMultiReader(readers ...io.Reader) (io.ReadSeeker, error) {
	r := make([]io.Reader, len(readers))
	sizes := make([]int64, len(readers))

	copy(r, readers)

	var totalSize int64
	for i, reader := range readers {
		seeker, ok := reader.(io.ReadSeeker)
		if !ok {
			return nil, fmt.Errorf("reader %d to SeekableMultiReader is not seekable", i)
		}

		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("size determination failed for reader %d to SeekableMultiReader: %v", i, err)
		}

		sizes[i] = size
		totalSize += size
	}

	return &seekableMultiReader{r, sizes, 0, totalSize}, nil
}

func (r *seekableMultiReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.currentOffset = offset
	case io.SeekCurrent:
		r.currentOffset += offset
	case io.SeekEnd:
		r.currentOffset = r.totalSize + offset
	default:
		return 0, errors.New("unsupported whence")
	}

	return r.currentOffset, nil
}

func (r *seekableMultiReader) Read(dst []byte) (int, error) {
	var readerStartAt int64

	for i, reader := range r.readers {
		if r.currentOffset < readerStartAt {
			// We want data from a previous reader (? HELP ?)
			readerStartAt += r.sizes[i]

			continue
		}

		if readerStartAt+r.sizes[i] < r.currentOffset {
			// We want data from a later reader
			readerStartAt += r.sizes[i]

			continue
		}

		// At least part of the data is in this reader

		seekable, ok := reader.(io.ReadSeeker)
		if !ok {
			return 0, errors.New("expected seekable reader but changed")
		}

		_, err := seekable.Seek(r.currentOffset-int64(readerStartAt), 0)
		if err != nil {
			return 0, fmt.Errorf("unexpected error while seeking: %v", err)
		}

		n, err := seekable.Read(dst)
		r.currentOffset += int64(n)

		if n > 0 || err != io.EOF {
			if err == io.EOF && r.currentOffset < r.totalSize {
				// More data left, hold that EOF
				err = nil
			}

			return n, err
		}

		readerStartAt += r.sizes[i]
	}

	return 0, io.EOF
}
