package rabbitmq

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type options struct {
	host          string
	port          int
	user          string
	password      string
	vhost         string
	exchange      string
	prefetchCount int
	ssl           bool
	caCert        string
	clientCert    string
	clientKey     string
}

var defaultConfig *options

func init() {
	defaultConfig = new(options)
	config.RegisterFlags(
		// Server flags
		&config.Flag{
			Name: "broker.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "0.0.0.0", "Host address to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.host = viper.GetString(flagName)
			},
		},

		&config.Flag{
			Name: "broker.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 5672, "Port used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				port = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "broker.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "User used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.user = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Password used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.password = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.vhost",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Virtual host used to connect the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.vhost = viper.GetString(flagName)
			},
		},

		&config.Flag{
			Name: "broker.exchange",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Exchange when publishing messages to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.exchange = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ssl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "If to connect to the rabbitmq server with tls")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.ssl = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "File path to the ca cert file of the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.caCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "File path to the client cert file used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.clientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "File path to the client key file used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.clientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.prefetch_count",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 2, "How many messages the rabbitmq server will deliver to a client before receiving delivery acknowledgements")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				defaultConfig.prefetchCount = viper.GetInt(flagName)
			},
		},
	)
}

// buildMQURI builds the MQ connection URI
func (cfg *options) clone() *options {
	return &options{
		host:          cfg.host,
		port:          cfg.port,
		user:          cfg.user,
		password:      cfg.password,
		vhost:         cfg.vhost,
		exchange:      cfg.exchange,
		prefetchCount: cfg.prefetchCount,
		ssl:           cfg.ssl,
		caCert:        cfg.caCert,
		clientCert:    cfg.clientCert,
		clientKey:     cfg.clientKey,
	}
}

// buildMQURI builds the MQ connection URI
func (cfg *options) buildMQURI() string {
	protocol := "amqp"
	if cfg.ssl {
		protocol = "amqps"
	}

	return fmt.Sprintf("%s://%s:%s@%s:%d%s", protocol, cfg.user, cfg.password, cfg.host, cfg.port, cfg.vhost)
}

func (cfg *options) setupTlsConfig() (*tls.Config, error) {
	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	tlsConfig := tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    systemCAs,
	}

	// Add CAs for broker and database
	if cfg.caCert != "" {
		cacert, err := os.ReadFile(cfg.caCert)
		if err != nil {
			return nil, err
		}
		if ok := tlsConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Warnln("No certs appended, using system certs only")
		}
	}

	if cfg.clientCert != "" && defaultConfig.clientKey != "" {
		cert, err := os.ReadFile(cfg.clientCert)
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(cfg.clientKey)
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
