package config

import (
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	sourceQueue   string
	archivedQueue string
	schemaPath    string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "broker.sourceQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The queue where the ingest service consumes ingest messaages from")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.archivedQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The queue where the ingest service publishes archive messaages to")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				archivedQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.schemaPath",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to JSON schemas to validate rabbitmq messages against")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
	)
}

func SourceQueue() string {
	return sourceQueue
}

func ArchivedQueue() string {
	return archivedQueue
}

func SchemaPath() string {
	return schemaPath
}
