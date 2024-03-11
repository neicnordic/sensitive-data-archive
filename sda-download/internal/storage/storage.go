// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package storage

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	log "github.com/sirupsen/logrus"
)

// Backend defines methods to be implemented by PosixBackend and S3Backend
type Backend interface {
	GetFileSize(filePath string) (int64, error)
	NewFileReader(filePath string) (io.ReadCloser, error)
	NewFileWriter(filePath string) (io.WriteCloser, error)
}

// Conf is a wrapper for the storage config
type Conf struct {
	Type  string
	S3    S3Conf
	Posix posixConf
}

type posixBackend struct {
	FileReader io.Reader
	FileWriter io.Writer
	Location   string
}

type posixConf struct {
	Location string
}

// NewBackend initiates a storage backend
func NewBackend(config Conf) (Backend, error) {
	switch config.Type {
	case "s3":
		return newS3Backend(config.S3)
	case "s3seekable":
		return newS3SeekableBackend(config.S3)

	default:
		return newPosixBackend(config.Posix)
	}
}

func newPosixBackend(config posixConf) (*posixBackend, error) {
	fileInfo, err := os.Stat(config.Location)

	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", config.Location)
	}

	return &posixBackend{Location: config.Location}, nil
}

// NewFileReader returns an io.Reader instance
func (pb *posixBackend) NewFileReader(filePath string) (io.ReadCloser, error) {
	if pb == nil {
		return nil, fmt.Errorf("Invalid posixBackend")
	}

	file, err := os.Open(filepath.Join(filepath.Clean(pb.Location), filePath))
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return file, nil
}

// NewFileWriter returns an io.Writer instance
func (pb *posixBackend) NewFileWriter(filePath string) (io.WriteCloser, error) {
	if pb == nil {
		return nil, fmt.Errorf("Invalid posixBackend")
	}

	file, err := os.OpenFile(filepath.Join(filepath.Clean(pb.Location), filePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return file, nil
}

// GetFileSize returns the size of the file
func (pb *posixBackend) GetFileSize(filePath string) (int64, error) {
	if pb == nil {
		return 0, fmt.Errorf("Invalid posixBackend")
	}

	stat, err := os.Stat(filepath.Join(filepath.Clean(pb.Location), filePath))
	if err != nil {
		log.Error(err)

		return 0, err
	}

	return stat.Size(), nil
}

type s3Backend struct {
	Client   *s3.S3
	Uploader *s3manager.Uploader
	Bucket   string
	Conf     *S3Conf
}

type s3CacheBlock struct {
	start  int64
	length int64
	data   []byte
}

type s3SeekableBackend struct {
	s3Backend
}

type s3SeekableReader struct {
	s3SeekableBackend
	currentOffset         int64
	local                 []s3CacheBlock
	filePath              string
	objectSize            int64
	lock                  sync.Mutex
	outstandingPrefetches []int64
	seeked                bool
	objectReader          io.Reader
}

// S3Conf stores information about the S3 storage backend
type S3Conf struct {
	URL               string
	Port              int
	AccessKey         string
	SecretKey         string
	Bucket            string
	Region            string
	UploadConcurrency int
	Chunksize         int
	Cacert            string
	NonExistRetryTime time.Duration
}

func newS3SeekableBackend(config S3Conf) (*s3SeekableBackend, error) {
	sb, err := newS3Backend(config)
	if err != nil {
		return nil, err
	}

	return &s3SeekableBackend{*sb}, nil
}

func newS3Backend(config S3Conf) (*s3Backend, error) {
	s3Transport := transportConfigS3(config)
	client := http.Client{Transport: s3Transport}
	s3Session := session.Must(session.NewSession(
		&aws.Config{
			Endpoint:         aws.String(fmt.Sprintf("%s:%d", config.URL, config.Port)),
			Region:           aws.String(config.Region),
			HTTPClient:       &client,
			S3ForcePathStyle: aws.Bool(true),
			DisableSSL:       aws.Bool(strings.HasPrefix(config.URL, "http:")),
			Credentials:      credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, ""),
		},
	))

	// Attempt to create a bucket, but we really expect an error here
	// (BucketAlreadyOwnedByYou)
	_, err := s3.New(s3Session).CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {

			if aerr.Code() != s3.ErrCodeBucketAlreadyOwnedByYou &&
				aerr.Code() != s3.ErrCodeBucketAlreadyExists {
				log.Error("Unexpected issue while creating bucket", err)
			}
		}
	}

	sb := &s3Backend{
		Bucket: config.Bucket,
		Uploader: s3manager.NewUploader(s3Session, func(u *s3manager.Uploader) {
			u.PartSize = int64(config.Chunksize)
			u.Concurrency = config.UploadConcurrency
			u.LeavePartsOnError = false
		}),
		Client: s3.New(s3Session),
		Conf:   &config}

	_, err = sb.Client.ListObjectsV2(&s3.ListObjectsV2Input{Bucket: &config.Bucket})

	if err != nil {
		return nil, err
	}

	return sb, nil
}

// NewFileReader returns an io.Reader instance
func (sb *s3Backend) NewFileReader(filePath string) (io.ReadCloser, error) {
	if sb == nil {
		return nil, fmt.Errorf("Invalid s3Backend")
	}

	r, err := sb.Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(sb.Bucket),
		Key:    aws.String(filePath),
	})

	retryTime := 2 * time.Minute
	if sb.Conf != nil {
		retryTime = sb.Conf.NonExistRetryTime
	}

	start := time.Now()
	for err != nil && time.Since(start) < retryTime {
		r, err = sb.Client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(sb.Bucket),
			Key:    aws.String(filePath),
		})
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		log.Error(err)

		return nil, err
	}

	return r.Body, nil
}

// NewFileWriter uploads the contents of an io.Reader to a S3 bucket
func (sb *s3Backend) NewFileWriter(filePath string) (io.WriteCloser, error) {
	if sb == nil {
		return nil, fmt.Errorf("Invalid s3Backend")
	}

	reader, writer := io.Pipe()
	go func() {

		_, err := sb.Uploader.Upload(&s3manager.UploadInput{
			Body:            reader,
			Bucket:          aws.String(sb.Bucket),
			Key:             aws.String(filePath),
			ContentEncoding: aws.String("application/octet-stream"),
		})

		if err != nil {
			_ = reader.CloseWithError(err)
		}
	}()

	return writer, nil
}

// GetFileSize returns the size of a specific object
func (sb *s3Backend) GetFileSize(filePath string) (int64, error) {
	if sb == nil {
		return 0, fmt.Errorf("Invalid s3Backend")
	}

	r, err := sb.Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sb.Bucket),
		Key:    aws.String(filePath)})

	start := time.Now()

	retryTime := 2 * time.Minute
	if sb.Conf != nil {
		retryTime = sb.Conf.NonExistRetryTime
	}

	// Retry on error up to five minutes to allow for
	// "slow writes' or s3 eventual consistency
	for err != nil && time.Since(start) < retryTime {
		r, err = sb.Client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(sb.Bucket),
			Key:    aws.String(filePath)})

		time.Sleep(1 * time.Second)

	}

	if err != nil {
		log.Errorln(err)

		return 0, err
	}

	return *r.ContentLength, nil
}

// transportConfigS3 is a helper method to setup TLS for the S3 client.
func transportConfigS3(config S3Conf) http.RoundTripper {
	cfg := new(tls.Config)

	// Enforce TLS1.2 or higher
	cfg.MinVersion = 2

	// Read system CAs
	var systemCAs, _ = x509.SystemCertPool()
	if reflect.DeepEqual(systemCAs, x509.NewCertPool()) {
		log.Debug("creating new CApool")
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if config.Cacert != "" {
		cacert, e := os.ReadFile(config.Cacert) // #nosec this file comes from our config
		if e != nil {
			log.Fatalf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	var trConfig http.RoundTripper = &http.Transport{
		TLSClientConfig:   cfg,
		ForceAttemptHTTP2: true}

	return trConfig
}

func (sb *s3SeekableBackend) NewFileReader(filePath string) (io.ReadCloser, error) {

	s := sb.s3Backend
	objectSize, err := s.GetFileSize(filePath)

	if err != nil {
		return nil, err
	}

	reader := &s3SeekableReader{
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

func (sb *s3SeekableBackend) GetFileSize(filePath string) (int64, error) {

	s := sb.s3Backend

	return s.GetFileSize(filePath)
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

	n := r.Conf.Chunksize

	if n >= 5*1024*1024 {
		return int64(n)
	}

	return 50 * 1024 * 1024
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

func (r *s3SeekableReader) Seek(offset int64, whence int) (int64, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	// Flag that we've seeked, so we don't use the mode optimised for reading from
	// start to end
	r.seeked = true

	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v- can't be negative when seeking from start", offset)
		}
		if offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset %v - beyond end of object (size %v)", offset, r.objectSize)
		}

		r.currentOffset = offset
		go r.prefetchAt(r.currentOffset)

		return offset, nil

	case io.SeekCurrent:
		if r.currentOffset+offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from %v would be be before start", offset, r.currentOffset)
		}
		if offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset - %v from %v would end up beyond of object %v", offset, r.currentOffset, r.objectSize)
		}

		r.currentOffset += offset
		go r.prefetchAt(r.currentOffset)

		return r.currentOffset, nil

	case io.SeekEnd:
		if r.objectSize+offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from end in %v bytes object, would be before file start", offset, r.objectSize)
		}
		if r.objectSize+offset > r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from end in %v bytes object", offset, r.objectSize)
		}

		r.currentOffset = r.objectSize + offset
		go r.prefetchAt(r.currentOffset)

		return r.currentOffset, nil
	}

	return r.currentOffset, fmt.Errorf("Bad whence")
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
		return 0, fmt.Errorf("Unexpected content range %v - expected prefix %v", object.ContentRange, responseRange)
	}

	b := bytes.Buffer{}
	_, err = io.Copy(&b, object.Body)

	// Add to cache
	cacheBytes := bytes.Clone(b.Bytes())
	r.local = append(r.local, s3CacheBlock{start, int64(len(cacheBytes)), cacheBytes})
	log.Infof("Stored into cache starting at %v, length %v", start, len(cacheBytes))

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
		if seeker, ok := reader.(io.ReadSeeker); !ok {
			return nil, fmt.Errorf("Reader %d to SeekableMultiReader is not seekable", i)
		} else {

			size, err := seeker.Seek(0, io.SeekEnd)
			if err != nil {
				return nil, fmt.Errorf("Size determination failed for reader %d to SeekableMultiReader: %v", i, err)
			}

			sizes[i] = size
			totalSize += size
		}
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
		return 0, fmt.Errorf("Unsupported whence")

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
			return 0, fmt.Errorf("Expected seekable reader but changed")
		}

		_, err := seekable.Seek(r.currentOffset-int64(readerStartAt), 0)
		if err != nil {
			return 0, fmt.Errorf("Unexpected error while seeking: %v", err)
		}

		n, err := seekable.Read(dst)
		r.currentOffset += int64(n)

		if n > 0 || err != io.EOF {
			if err == io.EOF && len(r.readers) > 0 {
				// More readers left, hold that EOF
				err = nil
			}

			return n, err
		}

		readerStartAt += r.sizes[i]
	}

	return 0, io.EOF
}
