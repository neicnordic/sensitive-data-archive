package config

import (
	"fmt"

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
			Name: "sourceQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "ingest", "The queue where the ingest service consumes ingest messaages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "archivedQueue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "archived", "The queue where the ingest service publishes archived messaages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				archivedQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "schemaType",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Path to JSON schemas to validate rabbitmq messages against")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				schemaType := viper.GetString("schemaType")
				switch schemaType {
				case "federated":
					schemaPath = "/schemas/federated/"
				case "isolated":
					schemaPath = "/schemas/isolated/"
				default:
					panic(fmt.Sprintf("schema.type '%s' not supported, needs: <federated|isolated>", schemaType))
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

func SetSchemaPath(path string) {
	schemaPath = path
}
