package config

import (
	"fmt"
	"time"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	targetPublicKey     string
	sourceQueue         string
	routingKey          string
	schemaPath          string
	reencryptTarget     string
	reencryptClientCert string
	reencryptClientKey  string
	reencryptCaCert     string
	reencryptTimeout    time.Duration
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "target_public_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to file containing the target public key for rotation")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				targetPublicKey = viper.GetString(flagName)
			},
		}, &config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "rotatekey", "The queue where the verify service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
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
		},
		&config.Flag{
			Name: "routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "archived", "The routing key which the rotatekey service publishes reverify messages with")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				routingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt.target",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The target where the Reencrypt service is hosted, see https://github.com/grpc/grpc/blob/master/doc/naming.md for more details on syntax")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				reencryptTarget = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt.client_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client cert file used when calling the the Reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				reencryptClientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt.client_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client key file used when calling the the Reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				reencryptClientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt.ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to the ca cert file used when calling the the Reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				reencryptCaCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt.timeout",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Duration(flagName, 30*time.Second, "The duration before timing out when calling the Reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				reencryptTimeout = viper.GetDuration(flagName)
			},
		},
	)
}

func TargetPublicKey() string {
	return targetPublicKey
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

func ReencryptTarget() string {
	return reencryptTarget
}
func ReencryptClientCert() string {
	return reencryptClientCert
}
func ReencryptClientKey() string {
	return reencryptClientKey
}
func ReencryptCaCert() string {
	return reencryptCaCert
}
func ReencryptTimeout() time.Duration {
	return reencryptTimeout
}
