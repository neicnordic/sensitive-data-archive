package config

import (
	"fmt"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	sourceQueue      string
	destinationQueue string
	schemaPath       string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "archived", "The queue where the verify service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "destination_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "verified", "The queue where the verify service publishes messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				destinationQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "schema_type",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Path to JSON schemas to validate broker messages against, available values: federated, isolated")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				schemaType := viper.GetString(flagName)
				switch schemaType {
				case "federated":
					schemaPath = "/schemas/federated/"
				case "isolated":
					schemaPath = "/schemas/isolated/"
				default:
					panic(fmt.Sprintf("schema type '%s' not supported, needs: <federated|isolated>", schemaType))
				}
			},
		},
	)
}

func SourceQueue() string {
	return sourceQueue
}

func DestinationQueue() string {
	return destinationQueue
}

func SchemaPath() string {
	return schemaPath
}
