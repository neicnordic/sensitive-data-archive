// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

// NewFileReadSeeker returns an io.ReadSeeker instance
func (pb *posixBackend) NewFileReadSeeker(filePath string) (io.ReadSeekCloser, error) {
	reader, err := pb.NewFileReader(filePath)
	if err != nil {
		return nil, err
	}

	seeker, ok := reader.(io.ReadSeekCloser)
	if !ok {
		return nil, errors.New("invalid posixBackend")
	}

	return seeker, nil
}

// s3CacheBlock is used to keep track of cached data
type s3CacheBlock struct {
	start  int64
	length int64
	data   []byte
}

// s3Reader is the vehicle to keep track of needed state for the reader
type s3Reader struct {
	s3Backend
	currentOffset         int64
	local                 []s3CacheBlock
	filePath              string
	objectSize            int64
	lock                  sync.Mutex
	outstandingPrefetches []int64
	seeked                bool
	objectReader          io.Reader
}

func (sb *s3Backend) NewFileReadSeeker(filePath string) (io.ReadSeekCloser, error) {
	objectSize, err := sb.GetFileSize(filePath)

	if err != nil {
		return nil, err
	}

	reader := &s3Reader{
		*sb,
		0,
		make([]s3CacheBlock, 0, 32),
		filePath,
		objectSize,
		sync.Mutex{},
		make([]int64, 0, 32),
		false,
		nil,
	}

	return reader, nil
}

func (r *s3Reader) Close() (err error) {
	return nil
}

func (r *s3Reader) pruneCache() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if len(r.local) < 16 {
		return
	}

	// Prune the cache
	keepfrom := len(r.local) - 8
	r.local = r.local[keepfrom:]
}

func (r *s3Reader) prefetchSize() int64 {
	n := r.Conf.Chunksize

	if n >= 5*1024*1024 {
		return int64(n)
	}

	return 50 * 1024 * 1024
}

func (r *s3Reader) prefetchAt(offset int64) {
	r.pruneCache()

	r.lock.Lock()
	defer r.lock.Unlock()

	if r.isPrefetching(offset) {
		// We're already fetching this
		return
	}

	// Check if we have the data in cache
	for _, p := range r.local {
		if offset >= p.start && offset < p.start+p.length {
			// At least part of the data is here
			return
		}
	}

	// Not found in cache, we should fetch the data
	bucket := aws.String(r.Bucket)
	key := aws.String(r.filePath)
	prefetchSize := r.prefetchSize()

	r.outstandingPrefetches = append(r.outstandingPrefetches, offset)

	r.lock.Unlock()

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+prefetchSize-1))

	object, err := r.Client.GetObject(&s3.GetObjectInput{
		Bucket: bucket,
		Key:    key,
		Range:  wantedRange,
	})

	r.lock.Lock()

	r.removeFromOutstanding(offset)

	if err != nil {
		return
	}

	responseRange := fmt.Sprintf("bytes %d-", r.currentOffset)

	if object.ContentRange == nil || !strings.HasPrefix(*object.ContentRange, responseRange) {
		// Unexpected content range - ignore
		return
	}

	if len(r.local) > 16 {
		// Don't cache anything more right now
		return
	}

	// Read into Buffer
	b := bytes.Buffer{}
	_, err = io.Copy(&b, object.Body)
	if err != nil {
		return
	}

	// Store in cache
	cacheBytes := b.Bytes()
	r.local = append(r.local, s3CacheBlock{offset, int64(len(cacheBytes)), cacheBytes})
}

func (r *s3Reader) Seek(offset int64, whence int) (int64, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	// Flag that we've seeked, so we don't use the mode optimised for reading from
	// start to end
	r.seeked = true

	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return r.currentOffset, fmt.Errorf("invalid offset %v- can't be negative when seeking from start", offset)
		}
		if offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("invalid offset %v - beyond end of object (size %v)", offset, r.objectSize)
		}

		r.currentOffset = offset
		go r.prefetchAt(r.currentOffset)

		return offset, nil
	case io.SeekCurrent:
		if r.currentOffset+offset < 0 {
			return r.currentOffset, fmt.Errorf("invalid offset %v from %v would be be before start", offset, r.currentOffset)
		}
		if offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("invalid offset - %v from %v would end up beyond of object %v", offset, r.currentOffset, r.objectSize)
		}

		r.currentOffset += offset
		go r.prefetchAt(r.currentOffset)

		return r.currentOffset, nil
	case io.SeekEnd:
		if r.objectSize+offset < 0 {
			return r.currentOffset, fmt.Errorf("invalid offset %v from end in %v bytes object, would be before file start", offset, r.objectSize)
		}
		if r.objectSize+offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("invalid offset %v from end in %v bytes object", offset, r.objectSize)
		}

		r.currentOffset = r.objectSize + offset
		go r.prefetchAt(r.currentOffset)

		return r.currentOffset, nil
	default:
		return r.currentOffset, errors.New("bad whence")
	}
}

// removeFromOutstanding removes a prefetch from the list of outstanding prefetches once it's no longer active
func (r *s3Reader) removeFromOutstanding(toRemove int64) {
	switch len(r.outstandingPrefetches) {
	case 0:
		// Nothing to do
	case 1:
		// Check if it's the one we should remove
		if r.outstandingPrefetches[0] == toRemove {
			r.outstandingPrefetches = r.outstandingPrefetches[:0]
		}

	default:
		remove := 0
		found := false
		for i, j := range r.outstandingPrefetches {
			if j == toRemove {
				remove = i
				found = true
			}
		}
		if found {
			r.outstandingPrefetches[remove] = r.outstandingPrefetches[len(r.outstandingPrefetches)-1]
			r.outstandingPrefetches = r.outstandingPrefetches[:len(r.outstandingPrefetches)-1]
		}
	}
}

// isPrefetching checks if the data is already being fetched
func (r *s3Reader) isPrefetching(offset int64) bool {
	// Walk through the outstanding prefetches
	for _, p := range r.outstandingPrefetches {
		if offset >= p && offset < p+r.prefetchSize() {
			// At least some of this read is already being fetched

			return true
		}
	}

	return false
}

// wholeReader is a helper for when we read the whole object
func (r *s3Reader) wholeReader(dst []byte) (int, error) {
	if r.objectReader == nil {
		// First call, setup a reader for the object
		object, err := r.Client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.filePath),
		})

		if err != nil {
			return 0, err
		}

		// Store for future use
		r.objectReader = object.Body
	}

	// Just use the reader, offset is handled in the caller
	return r.objectReader.Read(dst)
}

func (r *s3Reader) Read(dst []byte) (n int, err error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if !r.seeked {
		// If not seeked, guess that we use a whole object reader for performance

		n, err = r.wholeReader(dst)
		// We need to keep track of the position in the stream in case we seek
		r.currentOffset += int64(n)

		return n, err
	}

	if r.currentOffset >= r.objectSize {
		// For reading when there is no more data, just return EOF
		return 0, io.EOF
	}

	start := r.currentOffset

	// Walk through the cache
	for _, p := range r.local {
		if start >= p.start && start < p.start+p.length {
			// At least part of the data is here

			offsetInBlock := start - p.start

			// Pull out wanted data (as much as we have)
			n = copy(dst, p.data[offsetInBlock:])
			r.currentOffset += int64(n)

			// Prefetch the next bit
			go r.prefetchAt(r.currentOffset)

			return n, nil
		}
	}

	// Check if we're already fetching this data
	if r.isPrefetching(start) {
		// Return 0, nil to have the client retry

		return 0, nil
	}

	// Not found in cache, need to fetch data

	bucket := aws.String(r.Bucket)
	key := aws.String(r.filePath)

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", r.currentOffset, r.currentOffset+r.prefetchSize()-1))

	r.outstandingPrefetches = append(r.outstandingPrefetches, start)

	r.lock.Unlock()

	object, err := r.Client.GetObject(&s3.GetObjectInput{
		Bucket: bucket,
		Key:    key,
		Range:  wantedRange,
	})

	r.lock.Lock()

	r.removeFromOutstanding(start)

	if err != nil {
		return 0, err
	}

	responseRange := fmt.Sprintf("bytes %d-", r.currentOffset)

	if object.ContentRange == nil || !strings.HasPrefix(*object.ContentRange, responseRange) {
		return 0, fmt.Errorf("unexpected content range %v - expected prefix %v", object.ContentRange, responseRange)
	}

	b := bytes.Buffer{}
	_, err = io.Copy(&b, object.Body)

	// Add to cache
	cacheBytes := bytes.Clone(b.Bytes())
	r.local = append(r.local, s3CacheBlock{start, int64(len(cacheBytes)), cacheBytes})

	n, err = b.Read(dst)

	r.currentOffset += int64(n)
	go r.prefetchAt(r.currentOffset)

	return n, err
}

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
