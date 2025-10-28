package authenticator

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var jwtPubKeyPath, jwtPubKeyUrl string

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "jwt.pub-key-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url for fetching the elixir JWK for API authentication")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyUrl = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt.pub-key-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Local file containing jwk for authentication for API authentication")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyPath = viper.GetString(flagName)
			},
		},
	)
}
