package rabbitmq

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

// Config contains RabbitMQ connection and consumer settings.
type Config struct {
	Host          string
	Port          int
	User          string
	Password      string
	VHost         string
	Exchange      string
	PrefetchCount int
	SSL           bool
	CACert        string
	ClientCert    string
	ClientKey     string
	Timeout       int // Seconds between consumption retries.
	ShutdownGrace time.Duration
}

// DefaultConfig returns the defaults used by SDA services.
func DefaultConfig() Config {
	return Config{
		Host:          "rabbitmq",
		Port:          5672,
		VHost:         "/sda",
		Exchange:      "sda",
		PrefetchCount: 1,
		Timeout:       5,
		ShutdownGrace: 20 * time.Second,
	}
}

func (cfg Config) buildMQURI() string {
	u := &url.URL{
		Scheme: "amqp",
		Host:   cfg.Host,
	}

	if cfg.SSL {
		u.Scheme = "amqps"
	}
	if cfg.User != "" && cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	if cfg.Port != 0 {
		u.Host = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	}
	if cfg.VHost != "" {
		u.Path = cfg.VHost
	}

	return u.String()
}

func (cfg Config) setupTLSConfig() (*tls.Config, error) {
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	tlsConfig := tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    systemCAs,
	}

	if cfg.CACert != "" {
		cacert, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, err
		}
		if ok := tlsConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Warnln("No certs appended, using system certs only")
		}
	}

	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := os.ReadFile(cfg.ClientCert)
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(cfg.ClientKey)
		if err != nil {
			return nil, err
		}
		certs, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = append(tlsConfig.Certificates, certs)
	}

	return &tlsConfig, nil
}
