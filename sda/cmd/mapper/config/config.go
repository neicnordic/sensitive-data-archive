package config

import (
	"fmt"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	schemaPath  string
	sourceQueue string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "schema_type",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Schema type to validate incoming broker messages against, supported values: federated, isolated")
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
					panic(fmt.Sprintf("schema_type '%s' not supported, needs: <federated|isolated>", schemaType))
				}
			},
		}, &config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "mappings", "The queue where the mapper service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
	)
}

func SourceQueue() string {
	return sourceQueue
}

func SchemaPath() string {
	return schemaPath
}
