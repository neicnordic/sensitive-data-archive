package config

import (
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	apiPort           int
	validatorPaths    []string
	sdaApiUrl         string
	sdaApiToken       string
	validationWorkDir string
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
				apiPort = viper.GetInt(flagName)
			},
		}, &config.Flag{
			Name: "validator-paths",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.StringSlice(flagName, []string{}, "The paths to the available validators, in comma separated list")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				validatorPaths = viper.GetStringSlice(flagName)
			},
		}, &config.Flag{
			Name: "sda-api-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url to the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sdaApiUrl = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "sda-api-token",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Token to authenticate when calling the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sdaApiToken = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "validation-work-dir",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "/validators", "Directory where application will manage data to be used for validation")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				validationWorkDir = viper.GetString(flagName)
			},
		},
	)
}

func SdaApiToken() string {
	return sdaApiToken
}

func SdaApiUrl() string {
	return sdaApiUrl
}

func ApiPort() int {
	return apiPort
}
func ValidatorPaths() []string {
	return validatorPaths
}

func ValidationWorkDir() string {
	return validationWorkDir
}
