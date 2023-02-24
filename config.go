package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/neicnordic/sda-common/database"
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
	url       string
	readypath string
	accessKey string
	secretKey string
	bucket    string
	region    string
	cacert    string
}

// BrokerConfig stores information about the message broker
type BrokerConfig struct {
	host       string
	port       string
	user       string
	password   string
	vhost      string
	exchange   string
	routingKey string
	ssl        bool
	verifyPeer bool
	cacert     string
	clientCert string
	clientKey  string
	serverName string
}

// ServerConfig stores general server information
type ServerConfig struct {
	cert          string
	key           string
	jwtpubkeypath string
	jwtpubkeyurl  string
}

// Config is a parent object for all the different configuration parts
type Config struct {
	S3     S3Config
	Broker BrokerConfig
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
		ss := strings.Split(strings.TrimLeft(cp, "/"), "/")
		if ss[0] != "config" {
			ss = ss[:len(ss)-1]
		}
		viper.AddConfigPath(path.Join(ss...))
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
	s3.url = viper.GetString("aws.url")
	s3.accessKey = viper.GetString("aws.accessKey")
	s3.secretKey = viper.GetString("aws.secretKey")
	s3.bucket = viper.GetString("aws.bucket")

	// Optional settings
	if viper.IsSet("aws.readypath") {
		s3.readypath = viper.GetString("aws.readypath")
	}
	if viper.IsSet("aws.region") {
		s3.region = viper.GetString("aws.region")
	} else {
		s3.region = "us-east-1"
	}
	if viper.IsSet("aws.cacert") {
		s3.cacert = viper.GetString("aws.cacert")
	}

	c.S3 = s3

	// Setup broker
	b := BrokerConfig{}

	b.host = viper.GetString("broker.host")
	b.port = viper.GetString("broker.port")
	b.user = viper.GetString("broker.user")
	b.password = viper.GetString("broker.password")
	b.exchange = viper.GetString("broker.exchange")
	b.routingKey = viper.GetString("broker.routingKey")
	b.serverName = viper.GetString("broker.serverName")

	if viper.IsSet("broker.vhost") {
		if strings.HasPrefix(viper.GetString("broker.vhost"), "/") {
			b.vhost = viper.GetString("broker.vhost")
		} else {
			b.vhost = "/" + viper.GetString("broker.vhost")
		}
	} else {
		b.vhost = "/"
	}

	if viper.IsSet("broker.ssl") {
		b.ssl = viper.GetBool("broker.ssl")
	}
	if viper.IsSet("broker.verifyPeer") {
		b.verifyPeer = viper.GetBool("broker.verifyPeer")
		if b.verifyPeer {
			// Since verifyPeer is specified, these are required.
			if !(viper.IsSet("broker.clientCert") && viper.IsSet("broker.clientKey")) {
				return errors.New("when broker.verifyPeer is set both broker.clientCert and broker.clientKey is needed")
			}
			b.clientCert = viper.GetString("broker.clientCert")
			b.clientKey = viper.GetString("broker.clientKey")
		}
	}
	if viper.IsSet("broker.cacert") {
		b.cacert = viper.GetString("broker.cacert")
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
		s.jwtpubkeypath = viper.GetString("server.jwtpubkeypath")
	}

	if viper.IsSet("server.jwtpubkeyurl") {
		s.jwtpubkeyurl = viper.GetString("server.jwtpubkeyurl")
	}

	if viper.IsSet("server.cert") {
		s.cert = viper.GetString("server.cert")
	}
	if viper.IsSet("server.key") {
		s.key = viper.GetString("server.key")
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
	for _, cacert := range []string{c.Broker.cacert, c.S3.cacert} {
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
	if c.Broker.serverName != "" {
		cfg.ServerName = c.Broker.serverName
	}

	if c.Broker.verifyPeer {
		cert, e := os.ReadFile(c.Broker.clientCert)
		if e != nil {
			return nil, fmt.Errorf("failed to read client cert %q, reason: %v", c.Broker.clientKey, e)
		}
		key, e := os.ReadFile(c.Broker.clientKey)
		if e != nil {
			return nil, fmt.Errorf("failed to read client key %q, reason: %v", c.Broker.clientKey, e)
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

	if c.S3.cacert != "" {
		cacert, e := os.ReadFile(c.S3.cacert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}
