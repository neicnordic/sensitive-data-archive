package reader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3CacheBlock is used to keep track of cached data
type s3CacheBlock struct {
	start  int64
	length int64
	data   []byte
}

// s3SeekableReader is the vehicle to keep track of needed state for the reader
type s3SeekableReader struct {
	client                *s3.Client
	bucket                string
	currentOffset         int64
	local                 []s3CacheBlock
	filePath              string
	objectSize            int64
	lock                  sync.Mutex
	outstandingPrefetches []int64
	seeked                bool
	objectReader          io.Reader
	chunkSize             uint64
}

func (reader *Reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	endpoint, bucket, err := parseLocation(location)
	if err != nil {
		return nil, err
	}

	client, err := reader.getS3ClientForEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	objectSize, err := reader.getFileSize(ctx, client, bucket, filePath)
	if err != nil {
		return nil, err
	}

	var chunkSizeBytes uint64
	for _, e := range reader.endpoints {
		if e.Endpoint == endpoint {
			chunkSizeBytes = e.chunkSizeBytes

			break
		}
	}

	return &s3SeekableReader{
		client:                client,
		bucket:                bucket,
		currentOffset:         0,
		local:                 make([]s3CacheBlock, 0, 32),
		filePath:              filePath,
		objectSize:            objectSize,
		lock:                  sync.Mutex{},
		outstandingPrefetches: make([]int64, 0, 32),
		seeked:                false,
		objectReader:          nil,
		chunkSize:             chunkSizeBytes,
	}, nil
}

func (r *s3SeekableReader) Close() (err error) {
	return nil
}

func (r *s3SeekableReader) pruneCache() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if len(r.local) < 16 {
		return
	}

	// Prune the cache
	keepfrom := len(r.local) - 8
	r.local = r.local[keepfrom:]
}

func (r *s3SeekableReader) prefetchSize() int64 {
	// Type conversation safe as chunkSizeBytes checked to be between 5mb and 1gb (in bytes)
	//nolint:gosec // disable G115
	return int64(r.chunkSize)
}

func (r *s3SeekableReader) prefetchAt(offset int64) {
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
	prefetchSize := r.prefetchSize()

	r.outstandingPrefetches = append(r.outstandingPrefetches, offset)

	r.lock.Unlock()

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+prefetchSize-1))

	object, err := r.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.filePath),
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

func (r *s3SeekableReader) Seek(offset int64, whence int) (int64, error) {
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
func (r *s3SeekableReader) removeFromOutstanding(toRemove int64) {
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
func (r *s3SeekableReader) isPrefetching(offset int64) bool {
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
func (r *s3SeekableReader) wholeReader(dst []byte) (int, error) {
	if r.objectReader == nil {
		// First call, setup a reader for the object
		object, err := r.client.GetObject(context.Background(), &s3.GetObjectInput{
			Bucket: aws.String(r.bucket),
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

func (r *s3SeekableReader) Read(dst []byte) (n int, err error) {
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

	bucket := aws.String(r.bucket)
	key := aws.String(r.filePath)

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", r.currentOffset, r.currentOffset+r.prefetchSize()-1))

	r.outstandingPrefetches = append(r.outstandingPrefetches, start)

	r.lock.Unlock()

	object, err := r.client.GetObject(context.Background(), &s3.GetObjectInput{
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
