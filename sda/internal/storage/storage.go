// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	log "github.com/sirupsen/logrus"
)

// Backend defines methods to be implemented by PosixBackend, S3Backend and sftpBackend
type Backend interface {
	GetFileSize(filePath string, expectDelay bool) (int64, error)
	RemoveFile(filePath string) error
	NewFileReader(filePath string) (io.ReadCloser, error)
	NewFileWriter(filePath string) (io.WriteCloser, error)
}

// Conf is a wrapper for the storage config
type Conf struct {
	Type  string
	S3    S3Conf
	Posix posixConf
	SFTP  SftpConf
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
	case "sftp":
		return newSftpBackend(config.SFTP)
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
		return nil, fmt.Errorf("invalid posixBackend")
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
		return nil, fmt.Errorf("invalid posixBackend")
	}

	file, err := os.OpenFile(filepath.Join(filepath.Clean(pb.Location), filePath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return file, nil
}

// GetFileSize returns the size of the file
func (pb *posixBackend) GetFileSize(filePath string, _ bool) (int64, error) {
	if pb == nil {
		return 0, fmt.Errorf("invalid posixBackend")
	}

	stat, err := os.Stat(filepath.Join(filepath.Clean(pb.Location), filePath))
	if err != nil {
		log.Error(err)

		return 0, err
	}

	return stat.Size(), nil
}

// RemoveFile removes a file from a given path
func (pb *posixBackend) RemoveFile(filePath string) error {
	if pb == nil {
		return fmt.Errorf("invalid posixBackend")
	}

	err := os.Remove(filepath.Join(filepath.Clean(pb.Location), filePath))
	if err != nil {
		log.Error(err)

		return err
	}

	return nil
}

type s3Backend struct {
	Client   *s3.Client
	Uploader *manager.Uploader
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
	CAcert            string
	NonExistRetryTime time.Duration
	Readypath         string
}

func newS3Backend(conf S3Conf) (*s3Backend, error) {
	s3Client, err := NewS3Client(conf)
	if err != nil {
		return nil, err
	}

	sb := &s3Backend{
		Bucket: conf.Bucket,
		Client: s3Client,
		Conf:   &conf,
		Uploader: manager.NewUploader(s3Client, func(u *manager.Uploader) {
			u.PartSize = int64(conf.Chunksize)
			u.Concurrency = conf.UploadConcurrency
			u.LeavePartsOnError = false
		}),
	}

	err = CheckS3Bucket(conf.Bucket, s3Client)
	if err != nil {
		return sb, err
	}

	return sb, nil
}
func NewS3Client(conf S3Conf) (*s3.Client, error) {
	s3cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(conf.AccessKey, conf.SecretKey, "")),
		config.WithHTTPClient(&http.Client{Transport: transportConfigS3(conf)}),
	)
	if err != nil {
		return nil, err
	}

	endpoint := conf.URL
	if conf.Port != 0 {
		endpoint = fmt.Sprintf("%s:%d", conf.URL, conf.Port)
	}

	s3Client := s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.EndpointOptions.DisableHTTPS = strings.HasPrefix(conf.URL, "http:")
			o.Region = conf.Region
			o.UsePathStyle = true
		},
	)

	return s3Client, nil
}
func CheckS3Bucket(bucket string, s3Client *s3.Client) error {
	_, err := s3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{Bucket: &bucket})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			var bae *types.BucketAlreadyExists
			var baoby *types.BucketAlreadyOwnedByYou
			if errors.As(err, &bae) || errors.As(err, &baoby) {
				return nil
			}

			return fmt.Errorf("unexpected issue while creating bucket: %s", err.Error())
		}

		return fmt.Errorf("verifying bucket failed, check S3 configuration: %s", err.Error())
	}

	return nil
}

// NewFileReader returns an io.Reader instance
func (sb *s3Backend) NewFileReader(filePath string) (io.ReadCloser, error) {
	r, err := sb.Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &sb.Bucket,
		Key:    &filePath,
	})

	retryTime := 2 * time.Minute
	if sb.Conf != nil {
		retryTime = sb.Conf.NonExistRetryTime
	}

	start := time.Now()
	for err != nil && time.Since(start) < retryTime {
		if strings.Contains(err.Error(), "NoSuchKey:") {
			return nil, err
		}
		time.Sleep(1 * time.Second)
		r, err = sb.Client.GetObject(context.TODO(), &s3.GetObjectInput{
			Bucket: &sb.Bucket,
			Key:    &filePath,
		})
	}

	if err != nil {
		return nil, err
	}

	return r.Body, nil
}

// NewFileWriter uploads the contents of an io.Reader to a S3 bucket
func (sb *s3Backend) NewFileWriter(filePath string) (io.WriteCloser, error) {
	reader, writer := io.Pipe()
	go func() {
		_, err := sb.Uploader.Upload(context.TODO(), &s3.PutObjectInput{
			Body:            reader,
			Bucket:          &sb.Bucket,
			Key:             &filePath,
			ContentEncoding: aws.String("application/octet-stream"),
		})

		if err != nil {
			_ = reader.CloseWithError(err)
		}
	}()

	return writer, nil
}

// GetFileSize returns the size of a specific object
func (sb *s3Backend) GetFileSize(filePath string, expectDelay bool) (int64, error) {
	r, err := sb.Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: &sb.Bucket,
		Key:    &filePath,
	})

	start := time.Now()

	retryTime := 2 * time.Minute
	if sb.Conf != nil {
		retryTime = sb.Conf.NonExistRetryTime
	}

	// Retry on error up to five minutes to allow for
	// "slow writes' or s3 eventual consistency
	for err != nil && time.Since(start) < retryTime {
		if !expectDelay && (strings.Contains(err.Error(), "NoSuchKey:") || strings.Contains(err.Error(), "NotFound:")) {
			return 0, err
		}
		time.Sleep(1 * time.Second)

		r, err = sb.Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
			Bucket: &sb.Bucket,
			Key:    &filePath,
		})

	}

	if err != nil {
		return 0, err
	}

	return *r.ContentLength, nil
}

// RemoveFile removes an object from a bucket
func (sb *s3Backend) RemoveFile(filePath string) error {
	_, err := sb.Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: &sb.Bucket,
		Key:    &filePath,
	})
	if err != nil {
		return err
	}

	return nil
}

// transportConfigS3 is a helper method to setup TLS for the S3 client.
func transportConfigS3(conf S3Conf) http.RoundTripper {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}

	cfg.RootCAs = systemCAs

	if conf.CAcert != "" {
		cacert, e := os.ReadFile(conf.CAcert) // #nosec this file comes from our config
		if e != nil {
			log.Fatalf("failed to append %q to RootCAs: %v", cacert, e) // nolint # FIXME Fatal should only be called from main
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: true}
}

type sftpBackend struct {
	Connection *ssh.Client
	Client     *sftp.Client
	Conf       *SftpConf
}

// sftpConf stores information about the sftp storage backend
type SftpConf struct {
	Host       string
	Port       string
	UserName   string
	PemKeyPath string
	PemKeyPass string
	HostKey    string
}

func newSftpBackend(config SftpConf) (*sftpBackend, error) {
	// read in and parse pem key
	key, err := os.ReadFile(config.PemKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read from key file, %v", err)
	}

	var signer ssh.Signer
	if config.PemKeyPass == "" {
		signer, err = ssh.ParsePrivateKey(key)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(config.PemKeyPass))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key, %v", err)
	}

	// connect
	conn, err := ssh.Dial("tcp", config.Host+":"+config.Port,
		&ssh.ClientConfig{
			User:            config.UserName,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: TrustedHostKeyCallback(config.HostKey),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ssh connection, %v", err)
	}

	// create new SFTP client
	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to start sftp client, %v", err)
	}

	sfb := &sftpBackend{
		Connection: conn,
		Client:     client,
		Conf:       &config,
	}

	_, err = client.ReadDir("./")

	if err != nil {
		return nil, fmt.Errorf("failed to list files with sftp, %v", err)
	}

	return sfb, nil
}

// NewFileWriter returns an io.Writer instance for the sftp remote
func (sfb *sftpBackend) NewFileWriter(filePath string) (io.WriteCloser, error) {
	if sfb == nil {
		return nil, fmt.Errorf("invalid sftpBackend")
	}
	// Make remote directories
	parent := filepath.Dir(filePath)
	err := sfb.Client.MkdirAll(parent)
	if err != nil {
		return nil, fmt.Errorf("failed to create dir with sftp, %v", err)
	}

	file, err := sfb.Client.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("failed to create file with sftp, %v", err)
	}

	return file, nil
}

// GetFileSize returns the size of the file
func (sfb *sftpBackend) GetFileSize(filePath string, _ bool) (int64, error) {
	if sfb == nil {
		return 0, fmt.Errorf("invalid sftpBackend")
	}

	stat, err := sfb.Client.Lstat(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get file size with sftp, %v", err)
	}

	return stat.Size(), nil
}

// NewFileReader returns an io.Reader instance
func (sfb *sftpBackend) NewFileReader(filePath string) (io.ReadCloser, error) {
	if sfb == nil {
		return nil, fmt.Errorf("invalid sftpBackend")
	}

	file, err := sfb.Client.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file with sftp, %v", err)
	}

	return file, nil
}

// RemoveFile removes a file or an empty directory.
func (sfb *sftpBackend) RemoveFile(filePath string) error {
	if sfb == nil {
		return fmt.Errorf("invalid sftpBackend")
	}

	err := sfb.Client.Remove(filePath)
	if err != nil {
		return fmt.Errorf("failed to remove file with sftp, %v", err)
	}

	return nil
}

func TrustedHostKeyCallback(key string) ssh.HostKeyCallback {
	if key == "" {
		return func(_ string, _ net.Addr, k ssh.PublicKey) error {
			keyString := k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
			log.Warningf("host key verification is not in effect (Fix by adding trustedKey: %q)", keyString)

			return nil
		}
	}

	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		keyString := k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
		if ks := keyString; key != ks {
			return fmt.Errorf("host key verification expected %q but got %q", key, ks)
		}

		return nil
	}
}
