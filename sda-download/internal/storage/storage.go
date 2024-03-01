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
	currentOffset int64
	local         []s3CacheBlock
	filePath      string
	objectSize    int64
	lock          sync.Mutex
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

func (r *s3SeekableReader) prefetchAt(offset int64) {
	r.pruneCache()

	r.lock.Lock()
	defer r.lock.Unlock()

	for _, p := range r.local {
		if offset >= p.start && offset < p.start+p.length {
			// At least part of the data is here
			return
		}
	}

	bucket := aws.String(r.Bucket)
	key := aws.String(r.filePath)
	r.lock.Unlock()

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+int64(r.Conf.Chunksize)))

	object, err := r.Client.GetObject(&s3.GetObjectInput{
		Bucket: bucket,
		Key:    key,
		Range:  wantedRange,
	})

	r.lock.Lock()

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

	b := bytes.Buffer{}
	_, err = io.Copy(&b, object.Body)
	if err != nil {
		return
	}

	cacheBytes := bytes.Clone(b.Bytes())
	r.local = append(r.local, s3CacheBlock{offset, int64(len(cacheBytes)), cacheBytes})

}

func (r *s3SeekableReader) Seek(offset int64, whence int) (int64, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	switch whence {
	case 0:
		if offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v- can't be negative when seeking from start", offset)
		}
		if offset >= r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset %v - beyond end of object (size %v)", offset, r.objectSize)
		}

		r.currentOffset = offset
		go r.prefetchAt(r.currentOffset)

		return offset, nil
	case 1:
		if r.currentOffset+offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from %v would be be before start", offset, r.currentOffset)
		}
		if offset >= r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset - %v from %v would end up beyond of object %v", offset, r.currentOffset, r.objectSize)
		}

		r.currentOffset = offset
		go r.prefetchAt(r.currentOffset)

		return offset, nil

	case 2:

		if r.objectSize+offset < 0 {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from end in %v bytes object, would be before file start", offset, r.objectSize)
		}
		if r.objectSize+offset >= r.objectSize {
			return r.currentOffset, fmt.Errorf("Invalid offset %v from end in %v bytes object", offset, r.objectSize)
		}

		r.currentOffset = r.objectSize + offset
		go r.prefetchAt(r.currentOffset)

		return r.currentOffset, nil
	}

	return r.currentOffset, fmt.Errorf("Bad whence")
}

func (r *s3SeekableReader) Read(dst []byte) (n int, err error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	start := r.currentOffset

	// Walk through the cache
	for _, p := range r.local {
		if start >= p.start && start < p.start+p.length {
			// At least part of the data is here

			offsetInBlock := start - p.start
			wanted := int64(len(dst))

			if p.length-offsetInBlock < wanted {
				wanted = p.length - offsetInBlock
			}

			// Pull out wanted data (as much as we have)
			n = copy(dst, p.data[offsetInBlock:offsetInBlock+wanted])
			r.currentOffset += int64(n)

			// Prefetch the next bit
			go r.prefetchAt(r.currentOffset)

			return n, nil
		}
	}

	// Not found in cache, need to fetch data

	bucket := aws.String(r.Bucket)
	key := aws.String(r.filePath)

	wantedRange := aws.String(fmt.Sprintf("bytes=%d-%d", r.currentOffset, r.currentOffset+int64(len(dst))))
	r.lock.Unlock()

	// We don't bother putting the object into the cache, as we're going to read it all anyway

	object, err := r.Client.GetObject(&s3.GetObjectInput{
		Bucket: bucket,
		Key:    key,
		Range:  wantedRange,
	})

	r.lock.Lock()

	if err != nil {
		return 0, err
	}

	responseRange := fmt.Sprintf("bytes %d-", r.currentOffset)

	if object.ContentRange == nil || !strings.HasPrefix(*object.ContentRange, responseRange) {
		return 0, fmt.Errorf("Unexpected content range %v - expected prefix %v", object.ContentRange, responseRange)
	}

	n, err = object.Body.Read(dst)

	r.currentOffset += int64(n)
	go r.prefetchAt(r.currentOffset)

	return n, err
}

// seekableMultiReader is a helper struct to allow io.MultiReader to be used with a seekable reader
type seekableMultiReader struct {
	readers       []io.Reader
	sizes         []int64
	currentOffset int64
	allSeekable   bool
}

// SeekableMultiReader constructs a multireader that supports seeking
func SeekableMultiReader(readers ...io.Reader) io.ReadSeeker {

	r := make([]io.Reader, len(readers))
	s := make([]int64, len(readers))

	copy(r, readers)

	allSeekable := true
	for i, reader := range readers {
		if seeker, ok := reader.(io.ReadSeeker); !ok {
			allSeekable = false
		} else {
			s[i], _ = seeker.Seek(0, 2)
		}
	}

	return &seekableMultiReader{r, s, 0, allSeekable}
}

func (r *seekableMultiReader) Seek(offset int64, whence int) (int64, error) {

	if !r.allSeekable {
		return 0, fmt.Errorf("Not all readers are seekable")
	}

	switch whence {
	case 0:
		r.currentOffset = offset
	case 1:
		r.currentOffset += offset
	case 2:
		return 0, fmt.Errorf("Seeking from end not supported")

	default:
		return 0, fmt.Errorf("Unsupported whence")

	}

	return r.currentOffset, nil
}

func (r *seekableMultiReader) Read(dst []byte) (int, error) {

	if !r.allSeekable {
		// Modeled after io.MultiReader. Is it better to refuse at creation
		// if not all are seekable?

		for len(r.readers) > 0 {
			n, err := r.readers[0].Read(dst)
			if err == io.EOF {
				r.readers = r.readers[1:]
			}
			if n > 0 || err != io.EOF {
				if err == io.EOF && len(r.readers) > 0 {
					// More readers left, hold that EOF
					err = nil
				}

				return n, err
			}
		}
		// no readers left and no data to return
		return 0, io.EOF
	}

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
