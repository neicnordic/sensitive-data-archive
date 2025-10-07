package config

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	ApiPort        int
	ValidatorPaths []string
	SdaApiUrl      string
	SdaApiToken    string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "api-port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 0, "Port to host the ValidationAPI server at")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				ApiPort = viper.GetInt(flagName)
			},
		}, &config.Flag{
			Name: "validator-paths",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.StringSlice(flagName, []string{}, "The paths to the available validators, in comma separated list")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				ValidatorPaths = viper.GetStringSlice(flagName)
			},
		}, &config.Flag{
			Name: "sda-api-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url to the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				SdaApiUrl = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "sda-api-token",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Token to authenticate when calling the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				SdaApiToken = viper.GetString(flagName)
			},
		},
	)
}
