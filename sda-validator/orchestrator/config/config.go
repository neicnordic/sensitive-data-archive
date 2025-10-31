package config

import (
	"log"
	"strings"

	gounits "github.com/docker/go-units"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	apiPort           int
	validatorPaths    []string
	sdaAPIURL         string
	sdaAPIToken       string
	validationWorkDir string

	validationFileSizeLimit   int64
	jobWorkerCount            int
	jobQueue                  string
	jobPreparationWorkerCount int
	jobPreparationQueue       string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "validation-file-size-limit",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "100GB", "The human readable size limit of files in a single validation, this should equal the size of the size of the validation-work-dir. Supported abbreviations: B, kB, MB, GB, TB, PB, EB, ZB, YB")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				var err error
				validationFileSizeLimit, err = gounits.FromHumanSize(viper.GetString(flagName))
				if err != nil {
					log.Fatalf("failed to parse: %s due to: %v", viper.GetString(flagName), err)
				}
			},
		}, &config.Flag{
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
				flagSet.String(flagName, "", "The paths to the available validators, in comma separated list")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				validatorPaths = strings.Split(viper.GetString(flagName), ",")
			},
		}, &config.Flag{
			Name: "sda-api-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url to the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sdaAPIURL = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "sda-api-token",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Token to authenticate when calling the sda-api service")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sdaAPIToken = viper.GetString(flagName)
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
		}, &config.Flag{
			Name: "job-worker-count",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 2, "Amount of job workers to run")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jobWorkerCount = viper.GetInt(flagName)
			},
		}, &config.Flag{
			Name: "job-preparation-worker-count",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 1, "Amount of job preparation workers to run")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jobPreparationWorkerCount = viper.GetInt(flagName)
			},
		}, &config.Flag{
			Name: "job-queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The queue for validation job workers")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				jobQueue = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "job-preparation-queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The queue for job preparation workers")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				jobPreparationQueue = viper.GetString(flagName)
			},
		},
	)
}

func SdaAPIToken() string {
	return sdaAPIToken
}

func SdaAPIURL() string {
	return sdaAPIURL
}

func APIPort() int {
	return apiPort
}
func ValidatorPaths() []string {
	return validatorPaths
}

func ValidationWorkDir() string {
	return validationWorkDir
}

func JobPreparationWorkerCount() int {
	return jobPreparationWorkerCount
}
func JobPreparationQueue() string {
	return jobPreparationQueue
}
func JobWorkerCount() int {
	return jobWorkerCount
}
func JobQueue() string {
	return jobQueue
}
func ValidationFileSizeLimit() int64 {
	return validationFileSizeLimit
}
