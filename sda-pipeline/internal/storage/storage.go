// Package storage provides interface for storage areas, e.g. s3 or POSIX file system.
package storage

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	log "github.com/sirupsen/logrus"
)

// Backend defines methods to be implemented by PosixBackend, S3Backend and sftpBackend
type Backend interface {
	GetFileSize(filePath string) (int64, error)
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

// RemoveFile removes a file from a given path
func (pb *posixBackend) RemoveFile(filePath string) error {
	if pb == nil {
		return fmt.Errorf("Invalid posixBackend")
	}

	err := os.Remove(filepath.Join(filepath.Clean(pb.Location), filePath))
	if err != nil {
		log.Error(err)

		return err
	}

	return nil
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

// RemoveFile removes an object from a bucket
func (sb *s3Backend) RemoveFile(filePath string) error {
	if sb == nil {
		return fmt.Errorf("Invalid s3Backend")
	}

	_, err := sb.Client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(sb.Bucket),
		Key:    aws.String(filePath)})
	if err != nil {
		log.Error(err)

		return err
	}

	err = sb.Client.WaitUntilObjectNotExists(&s3.HeadObjectInput{
		Bucket: aws.String(sb.Bucket),
		Key:    aws.String(filePath)})
	if err != nil {
		return err
	}

	return nil
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
		return nil, fmt.Errorf("Failed to read from key file, %v", err)
	}

	var signer ssh.Signer
	if config.PemKeyPass == "" {
		signer, err = ssh.ParsePrivateKey(key)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(config.PemKeyPass))
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to parse private key, %v", err)
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
		return nil, fmt.Errorf("Failed to start ssh connection, %v", err)
	}

	// create new SFTP client
	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("Failed to start sftp client, %v", err)
	}

	sfb := &sftpBackend{
		Connection: conn,
		Client:     client,
		Conf:       &config,
	}

	_, err = client.ReadDir("./")

	if err != nil {
		return nil, fmt.Errorf("Failed to list files with sftp, %v", err)
	}

	return sfb, nil
}

// NewFileWriter returns an io.Writer instance for the sftp remote
func (sfb *sftpBackend) NewFileWriter(filePath string) (io.WriteCloser, error) {
	if sfb == nil {
		return nil, fmt.Errorf("Invalid sftpBackend")
	}
	// Make remote directories
	parent := filepath.Dir(filePath)
	err := sfb.Client.MkdirAll(parent)
	if err != nil {
		return nil, fmt.Errorf("Failed to create dir with sftp, %v", err)
	}

	file, err := sfb.Client.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("Failed to create file with sftp, %v", err)
	}

	return file, nil
}

// GetFileSize returns the size of the file
func (sfb *sftpBackend) GetFileSize(filePath string) (int64, error) {
	if sfb == nil {
		return 0, fmt.Errorf("Invalid sftpBackend")
	}

	stat, err := sfb.Client.Lstat(filePath)
	if err != nil {
		return 0, fmt.Errorf("Failed to get file size with sftp, %v", err)
	}

	return stat.Size(), nil
}

// NewFileReader returns an io.Reader instance
func (sfb *sftpBackend) NewFileReader(filePath string) (io.ReadCloser, error) {
	if sfb == nil {
		return nil, fmt.Errorf("Invalid sftpBackend")
	}

	file, err := sfb.Client.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to open file with sftp, %v", err)
	}

	return file, nil
}

// RemoveFile removes a file or an empty directory.
func (sfb *sftpBackend) RemoveFile(filePath string) error {
	if sfb == nil {
		return fmt.Errorf("Invalid sftpBackend")
	}

	err := sfb.Client.Remove(filePath)
	if err != nil {
		return fmt.Errorf("Failed to remove file with sftp, %v", err)
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
