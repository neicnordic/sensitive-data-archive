package config

import (
	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	sourceQueue         string
	accessionRoutingKey string
	cancelRoutingKey    string
	ingestRoutingKey    string
	mappingRoutingKey   string
	releaseRoutingKey   string
	deprecateRoutingKey string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "from_cega", "The queue where the verify service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "accession_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "accession", "The routing key the intercept service forwards \"accession\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				accessionRoutingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "ingest_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "ingest", "The routing key the intercept service forwards \"ingest\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				ingestRoutingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "cancel_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "ingest", "The routing key the intercept service forwards \"cancel\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				cancelRoutingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "mapping_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "mappings", "The routing key the intercept service forwards \"mapping\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				mappingRoutingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "release_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "mappings", "The routing key the intercept service forwards \"release\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				releaseRoutingKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "deprecate_routing_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "mappings", "The routing key the intercept service forwards \"deprecate\" type messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				deprecateRoutingKey = viper.GetString(flagName)
			},
		},
	)
}

func SourceQueue() string {
	return sourceQueue
}
func AccessionRoutingKey() string {
	return accessionRoutingKey
}
func CancelRoutingKey() string {
	return cancelRoutingKey
}
func IngestRoutingKey() string {
	return ingestRoutingKey
}
func MappingRoutingKey() string {
	return mappingRoutingKey
}
func ReleaseRoutingKey() string {
	return releaseRoutingKey
}
func DeprecateRoutingKey() string {
	return deprecateRoutingKey
}
