package config

import (
	"fmt"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	sourceQueue string
	schemaPath  string

	syncC4ghPubKeyPath    string
	syncDatasetWithPrefix string
	remoteURL             string
	remoteUser            string
	remotePassword        string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "source_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "ingest", "The queue where the sync service consumes messages from")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sourceQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "sync.c4gh_pub_key_path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to c4gh pub key file with which to encrypt files being synced")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				syncC4ghPubKeyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "schema_type",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Schema type used to validate incoming messages against, supported values: federated, isolated")
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
			Name: "sync.dataset_with_prefix",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Only sync datasets which has this prefix in the accession")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				syncDatasetWithPrefix = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "remote.url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "URL to the a remote http server to notify about a dataset being synced, if not populated no http notification will take place")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				remoteURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "remote.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Username used for basic auth when calling remote, if not set http notification to remote.url will be done with out basic auth")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				remoteUser = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "remote.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Password used for basic auth when calling remote, if not set http notification to remote.url will be done with out basic auth")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				remotePassword = viper.GetString(flagName)
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

func SyncDatasetWithPrefix() string {
	return syncDatasetWithPrefix
}
func SyncC4ghPubKeyPath() string {
	return syncC4ghPubKeyPath
}
func RemoteURL() string {
	return remoteURL
}
func RemoteUser() string {
	return remoteUser
}
func RemotePassword() string {
	return remotePassword
}
