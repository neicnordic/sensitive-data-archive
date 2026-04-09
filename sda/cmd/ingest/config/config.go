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
			Name: "ingest.sourceQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "ingest", "The queue where the ingest service consumes ingest messaages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "ingest.archivedQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "archived", "The queue where the ingest service publishes archived messaages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				archivedQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "ingest.schemaPath",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "/schemas/isolated/", "Path to JSON schemas to validate rabbitmq messages against")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				if viper.IsSet(flagName) {
					schemaPath = viper.GetString(flagName)

					return
				}

				if viper.GetString("schema.type") == "federated" {
					schemaPath = "/schemas/federated/"
				} else {
					schemaPath = "/schemas/isolated/"
				}
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
