package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	requiredConfVars = []string{
		"aws.url", "aws.accessKey", "aws.secretKey", "aws.bucket",
		"broker.host", "broker.port", "broker.user", "broker.password", "broker.vhost", "broker.exchange", "broker.routingKey",
	}
)

// S3Config stores information about the S3 backend
type S3Config struct {
	URL       string
	Readypath string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	CAcert    string
}

// ServerConfig stores general server information
type ServerConfig struct {
	Cert          string
	Key           string
	Jwtpubkeypath string
	Jwtpubkeyurl  string
}

// Config is a parent object for all the different configuration parts
type Config struct {
	S3     S3Config
	Broker broker.MQConf
	Server ServerConfig
	DB     database.DBConf
}

// NewConfig initializes and parses the config file and/or environment using
// the viper library.
func NewConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	if viper.IsSet("server.confPath") {
		cp := viper.GetString("server.confPath")
		if !strings.HasSuffix(cp, "/") {
			cp += "/"
		}
		viper.AddConfigPath(cp)
	}
	if viper.IsSet("server.confFile") {
		viper.SetConfigFile(viper.GetString("server.confFile"))
	}
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Infoln("No config file found, using ENVs only")
		} else {
			return nil, err
		}
	}

	requiredConfVars = []string{
		"broker.host", "broker.port", "broker.user", "broker.password", "broker.exchange", "broker.routingkey", "aws.url", "aws.accesskey", "aws.secretkey", "aws.bucket",
	}

	for _, s := range requiredConfVars {
		if !viper.IsSet(s) {
			return nil, fmt.Errorf("%s not set", s)
		}
	}

	if viper.IsSet("log.format") {
		if viper.GetString("log.format") == "json" {
			log.SetFormatter(&log.JSONFormatter{})
			log.Info("The logs format is set to JSON")
		}
	}

	if viper.IsSet("log.level") {
		stringLevel := viper.GetString("log.level")
		intLevel, err := log.ParseLevel(stringLevel)
		if err != nil {
			log.Infof("Log level '%s' not supported, setting to 'trace'", stringLevel)
			intLevel = log.TraceLevel
		}
		log.SetLevel(intLevel)
		log.Infof("Setting log level to '%s'", stringLevel)
	}

	c := &Config{}
	err := c.readConfig()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Config) readConfig() error {
	s3 := S3Config{}

	// All these are required
	s3.URL = viper.GetString("aws.url")
	s3.AccessKey = viper.GetString("aws.accessKey")
	s3.SecretKey = viper.GetString("aws.secretKey")
	s3.Bucket = viper.GetString("aws.bucket")

	// Optional settings
	if viper.IsSet("aws.readypath") {
		s3.Readypath = viper.GetString("aws.readypath")
	}
	if viper.IsSet("aws.region") {
		s3.Region = viper.GetString("aws.region")
	} else {
		s3.Region = "us-east-1"
	}
	if viper.IsSet("aws.cacert") {
		s3.CAcert = viper.GetString("aws.cacert")
	}

	c.S3 = s3

	// Setup broker
	b := broker.MQConf{}

	b.Host = viper.GetString("broker.host")
	b.Port, _ = strconv.Atoi(viper.GetString("broker.port"))
	b.User = viper.GetString("broker.user")
	b.Password = viper.GetString("broker.password")
	b.Exchange = viper.GetString("broker.exchange")
	b.RoutingKey = viper.GetString("broker.routingKey")
	b.ServerName = viper.GetString("broker.serverName")

	if viper.IsSet("broker.vhost") {
		if strings.HasPrefix(viper.GetString("broker.vhost"), "/") {
			b.Vhost = viper.GetString("broker.vhost")
		} else {
			b.Vhost = "/" + viper.GetString("broker.vhost")
		}
	} else {
		b.Vhost = "/"
	}

	if viper.IsSet("broker.ssl") {
		b.Ssl = viper.GetBool("broker.ssl")
	}
	if viper.IsSet("broker.verifyPeer") {
		b.VerifyPeer = viper.GetBool("broker.verifyPeer")
		if b.VerifyPeer {
			// Since verifyPeer is specified, these are required.
			if !(viper.IsSet("broker.clientCert") && viper.IsSet("broker.clientKey")) {
				return errors.New("when broker.verifyPeer is set both broker.clientCert and broker.clientKey is needed")
			}
			b.ClientCert = viper.GetString("broker.clientCert")
			b.ClientKey = viper.GetString("broker.clientKey")
		}
	}
	if viper.IsSet("broker.cacert") {
		b.CACert = viper.GetString("broker.cacert")
	}

	c.Broker = b

	// Setup psql db
	c.DB.Host = viper.GetString("db.host")
	c.DB.Port = viper.GetInt("db.port")
	c.DB.User = viper.GetString("db.user")
	c.DB.Password = viper.GetString("db.password")
	c.DB.Database = viper.GetString("db.database")
	if viper.IsSet("db.cacert") {
		c.DB.CACert = viper.GetString("db.cacert")
	}
	c.DB.SslMode = viper.GetString("db.sslmode")
	if c.DB.SslMode == "verify-full" {
		// Since verify-full is specified, these are required.
		if !(viper.IsSet("db.clientCert") && viper.IsSet("db.clientKey")) {
			return errors.New("when db.sslMode is set to verify-full both db.clientCert and db.clientKey are needed")
		}
		c.DB.ClientCert = viper.GetString("db.clientcert")
		c.DB.ClientKey = viper.GetString("db.clientkey")
	}

	// Setup server
	s := ServerConfig{}

	if !(viper.IsSet("server.jwtpubkeypath") || viper.IsSet("server.jwtpubkeyurl")) {
		return errors.New("either server.pubkeypath or server.jwtpubkeyurl should be present to start the service")
	}

	// Token authentication
	if viper.IsSet("server.jwtpubkeypath") {
		s.Jwtpubkeypath = viper.GetString("server.jwtpubkeypath")
	}

	if viper.IsSet("server.jwtpubkeyurl") {
		s.Jwtpubkeyurl = viper.GetString("server.jwtpubkeyurl")
	}

	if viper.IsSet("server.cert") {
		s.Cert = viper.GetString("server.cert")
	}
	if viper.IsSet("server.key") {
		s.Key = viper.GetString("server.key")
	}

	c.Server = s

	return nil
}

// TLSConfigBroker is a helper method to setup TLS for the message broker
func TLSConfigBroker(c *Config) (*tls.Config, error) {
	cfg := new(tls.Config)

	log.Debug("setting up TLS for broker connection")

	// Enforce TLS1.2 or higher
	cfg.MinVersion = 2

	// Read system CAs
	var systemCAs, _ = x509.SystemCertPool()
	if reflect.DeepEqual(systemCAs, x509.NewCertPool()) {
		log.Debug("creating new CApool")
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	// Add CAs for broker and s3
	for _, cacert := range []string{c.Broker.CACert, c.S3.CAcert} {
		if cacert == "" {
			continue
		}

		cacert, e := os.ReadFile(cacert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	// This might be a bad thing to do globally, but we'll see.
	if c.Broker.ServerName != "" {
		cfg.ServerName = c.Broker.ServerName
	}

	if c.Broker.VerifyPeer {
		cert, e := os.ReadFile(c.Broker.ClientCert)
		if e != nil {
			return nil, fmt.Errorf("failed to read client cert %q, reason: %v", c.Broker.ClientKey, e)
		}
		key, e := os.ReadFile(c.Broker.ClientKey)
		if e != nil {
			return nil, fmt.Errorf("failed to read client key %q, reason: %v", c.Broker.ClientKey, e)
		}
		if certs, e := tls.X509KeyPair(cert, key); e == nil {
			cfg.Certificates = append(cfg.Certificates, certs)
		}
	}

	return cfg, nil
}

// TLSConfigProxy is a helper method to setup TLS for the S3 backend.
func TLSConfigProxy(c *Config) (*tls.Config, error) {
	cfg := new(tls.Config)

	log.Debug("setting up TLS for S3 connection")

	// Enforce TLS1.2 or higher
	cfg.MinVersion = 2

	// Read system CAs
	var systemCAs, _ = x509.SystemCertPool()
	if reflect.DeepEqual(systemCAs, x509.NewCertPool()) {
		log.Debug("creating new CApool")
		systemCAs = x509.NewCertPool()
	}
	cfg.RootCAs = systemCAs

	if c.S3.CAcert != "" {
		cacert, e := os.ReadFile(c.S3.CAcert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}
