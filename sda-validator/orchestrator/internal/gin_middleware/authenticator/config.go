package authenticator

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type authenticatorConfig struct {
	jwtPubKeyPath, jwtPubKeyUrl string
}

var conf = &authenticatorConfig{}

func init() {

	config.RegisterFlags(
		&config.Flag{
			Name: "jwt.pub-key-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url for fetching the elixir JWK for API authentication")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				conf.jwtPubKeyUrl = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt.pub-key-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Local file containing jwk for authentication for API authentication")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				conf.jwtPubKeyPath = viper.GetString(flagName)
			},
		},
	)
}

func JwtPubKeyPath(v string) func(*authenticatorConfig) {
	return func(c *authenticatorConfig) {
		c.jwtPubKeyPath = v
	}
}
func JwtPubKeyUrl(v string) func(*authenticatorConfig) {
	return func(c *authenticatorConfig) {
		c.jwtPubKeyUrl = v
	}
}

func (c *authenticatorConfig) clone() *authenticatorConfig {
	return &authenticatorConfig{
		jwtPubKeyPath: c.jwtPubKeyPath,
		jwtPubKeyUrl:  c.jwtPubKeyUrl,
	}
}
