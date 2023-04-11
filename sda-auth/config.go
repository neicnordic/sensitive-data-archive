package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// ElixirConfig stores the config about the elixir oidc endpoint
type ElixirConfig struct {
	ID            string
	Provider      string
	RedirectURL   string
	RevocationURL string
	Secret        string
	jwkURL        string
}

// CegaConfig stores information about the cega endpoint
type CegaConfig struct {
	AuthURL string
	ID      string
	Secret  string
}

// CORSConfig stores information about cross-origin resource sharing
type CORSConfig struct {
	AllowOrigin      string
	AllowMethods     string
	AllowHeaders     string
	AllowCredentials bool
}

// ServerConfig stores general server information
type ServerConfig struct {
	Cert string
	Key  string
	CORS CORSConfig
}

// Config is a parent object for all the different configuration parts
type Config struct {
	Elixir          ElixirConfig
	Cega            CegaConfig
	JwtIssuer       string
	JwtPrivateKey   string
	JwtSignatureAlg string
	Server          ServerConfig
	S3Inbox         string
	ResignJwt       bool
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
		ss := strings.Split(strings.TrimLeft(cp, "/"), "/")
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

	c := &Config{}
	err := c.readConfig()

	return c, err
}

func (c *Config) readConfig() error {
	c.JwtPrivateKey = viper.GetString("JwtPrivateKey")
	c.JwtSignatureAlg = viper.GetString("JwtSignatureAlg")
	c.JwtIssuer = viper.GetString("jwtIssuer")

	viper.SetDefault("ResignJwt", true)
	c.ResignJwt = viper.GetBool("resignJwt")

	// Setup elixir
	elixir := ElixirConfig{}

	elixir.ID = viper.GetString("elixir.id")
	elixir.Provider = viper.GetString("elixir.provider")
	elixir.RedirectURL = viper.GetString("elixir.redirectUrl")
	elixir.Secret = viper.GetString("elixir.secret")
	if viper.IsSet("elixir.jwkPath") {
		elixir.jwkURL = elixir.Provider + viper.GetString("elixir.jwkPath")
	}

	c.Elixir = elixir

	// Setup cega
	cega := CegaConfig{}

	cega.AuthURL = viper.GetString("cega.authUrl")
	cega.ID = viper.GetString("cega.id")
	cega.Secret = viper.GetString("cega.secret")

	c.Cega = cega

	// Read CORS settings
	cors := CORSConfig{AllowCredentials: false}
	if viper.IsSet("cors.origins") {
		cors.AllowOrigin = viper.GetString("cors.origins")
	}
	if viper.IsSet("cors.methods") {
		cors.AllowMethods = viper.GetString("cors.methods")
	}
	if viper.IsSet("cors.headers") {
		cors.AllowHeaders = viper.GetString("cors.headers")
	}
	if viper.IsSet("cors.credentials") {
		cors.AllowCredentials = viper.GetBool("cors.credentials")
	}

	// Setup server
	s := ServerConfig{CORS: cors}

	if viper.IsSet("server.cert") {
		s.Cert = viper.GetString("server.cert")
	}
	if viper.IsSet("server.key") {
		s.Key = viper.GetString("server.key")
	}

	c.Server = s

	c.S3Inbox = viper.GetString("s3Inbox")

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
			log.Printf("Log level '%s' not supported, setting to 'trace'", stringLevel)
			intLevel = log.TraceLevel
		}
		log.SetLevel(intLevel)
		log.Printf("Setting log level to '%s'", stringLevel)
	}

	for _, s := range []string{"jwtIssuer", "JwtPrivateKey", "JwtSignatureAlg", "s3Inbox"} {
		if viper.GetString(s) == "" {
			return fmt.Errorf("%s not set", s)
		}
	}

	if _, err := os.Stat(c.JwtPrivateKey); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Missing private key file, reason: '%s'", err)
	}

	return nil
}
