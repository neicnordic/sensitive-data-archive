// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package storage

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	NewFileReadSeeker(filePath string) (io.ReadSeekCloser, error)
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
		return nil, errors.New("Invalid posixBackend")
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
		return nil, errors.New("Invalid posixBackend")
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
		return 0, errors.New("Invalid posixBackend")
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
		return nil, errors.New("Invalid s3Backend")
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
		return nil, errors.New("Invalid s3Backend")
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
		return 0, errors.New("Invalid s3Backend")
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

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if config.Cacert != "" {
		cacert, e := os.ReadFile(config.Cacert) // #nosec this file comes from our config
		if e != nil {
			log.Fatalf("failed to append %q to RootCAs: %v", cacert, e) // nolint # FIXME Fatal should only be called from main
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
