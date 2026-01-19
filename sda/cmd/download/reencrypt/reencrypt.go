// Package reencrypt provides a client for the gRPC reencrypt service.
package reencrypt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client provides methods for re-encrypting crypt4gh headers.
type Client struct {
	host       string
	port       int
	timeout    time.Duration
	caCert     string
	clientCert string
	clientKey  string

	mu   sync.Mutex
	conn *grpc.ClientConn
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithTimeout sets the timeout for gRPC requests.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithTLS enables TLS with the given CA certificate, client cert, and client key.
func WithTLS(caCert, clientCert, clientKey string) ClientOption {
	return func(c *Client) {
		c.caCert = caCert
		c.clientCert = clientCert
		c.clientKey = clientKey
	}
}

// NewClient creates a new reencrypt client.
func NewClient(host string, port int, opts ...ClientOption) *Client {
	c := &Client{
		host:    host,
		port:    port,
		timeout: 10 * time.Second, // default timeout
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// connect establishes the gRPC connection if not already connected.
func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil
	}

	opts, err := c.buildDialOptions()
	if err != nil {
		return err
	}

	address := fmt.Sprintf("%s:%d", c.host, c.port)
	log.Debugf("connecting to reencrypt service at: %s", address)

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to reencrypt service: %w", err)
	}

	c.conn = conn

	return nil
}

// buildDialOptions creates gRPC dial options based on TLS configuration.
func (c *Client) buildDialOptions() ([]grpc.DialOption, error) {
	if c.clientKey == "" || c.clientCert == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}

	creds, err := c.buildTLSCredentials()
	if err != nil {
		return nil, err
	}

	return []grpc.DialOption{grpc.WithTransportCredentials(creds)}, nil
}

// buildTLSCredentials creates TLS credentials for the gRPC connection.
func (c *Client) buildTLSCredentials() (credentials.TransportCredentials, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Warnf("failed to read system CAs: %v, using empty pool", err)
		rootCAs = x509.NewCertPool()
	}

	if c.caCert != "" {
		if err := c.appendCACert(rootCAs); err != nil {
			return nil, err
		}
	}

	certs, err := tls.LoadX509KeyPair(c.clientCert, c.clientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client key pair: %w", err)
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{certs},
		MinVersion:   tls.VersionTLS13,
		RootCAs:      rootCAs,
	}), nil
}

// appendCACert reads and appends the CA certificate to the pool.
func (c *Client) appendCACert(pool *x509.CertPool) error {
	caCertBytes, err := os.ReadFile(c.caCert)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	if !pool.AppendCertsFromPEM(caCertBytes) {
		return errors.New("failed to append CA certificate")
	}

	return nil
}

// ReencryptHeader re-encrypts a crypt4gh header with a new public key.
// The publicKey should be base64 encoded.
func (c *Client) ReencryptHeader(ctx context.Context, oldHeader []byte, publicKey string) ([]byte, error) {
	if err := c.connect(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	client := re.NewReencryptClient(c.conn)

	res, err := client.ReencryptHeader(ctx, &re.ReencryptRequest{
		Publickey: publicKey,
		Oldheader: oldHeader,
	})
	if err != nil {
		return nil, fmt.Errorf("reencrypt request failed: %w", err)
	}

	return res.GetHeader(), nil
}

// ReencryptHeaderWithEditList re-encrypts a crypt4gh header with a new public key
// and a data edit list for partial file access.
func (c *Client) ReencryptHeaderWithEditList(ctx context.Context, oldHeader []byte, publicKey string, dataEditList []uint64) ([]byte, error) {
	if err := c.connect(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	client := re.NewReencryptClient(c.conn)

	res, err := client.ReencryptHeader(ctx, &re.ReencryptRequest{
		Publickey:    publicKey,
		Oldheader:    oldHeader,
		Dataeditlist: dataEditList,
	})
	if err != nil {
		return nil, fmt.Errorf("reencrypt request failed: %w", err)
	}

	return res.GetHeader(), nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil

		return err
	}

	return nil
}
