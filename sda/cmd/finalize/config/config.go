package config

import (
	"fmt"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	sourceQueue string
	routingKey  string
	schemaPath  string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "accession", "The queue where the service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "completed", "The routing key used by the service to publish messages")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				routingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "schema_type",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "JSON schemas to validate rabbitmq messages against")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				schemaType := viper.GetString("schema_type")
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

func RoutingKey() string {
	return routingKey
}

func SchemaPath() string {
	return schemaPath
}

func SetSchemaPath(path string) {
	schemaPath = path
}
