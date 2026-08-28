package config

import (
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	destinationQueue    string
	s3InboxEndpoint     string
	s3InboxAccessKey    string
	s3InboxSecretKey    string
	s3InboxBucket       string
	s3InboxRegion       string
	s3InboxCaCert       string
	s3InboxReadyPath    string
	serverKey           string
	serverJwtPubKeyPath string
	serverJwtPubKeyURL  string
	serverCert          string
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "destination_queue",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "inbox", "The queue where the s3inbox service publishes messages to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				destinationQueue = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.endpoint",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The endpoint to the s3 serving as the inbox")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				s3InboxEndpoint = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.access_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Access key used to authenticate towards the s3 endpoint")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				s3InboxAccessKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.secret_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Secret key used to authenticate towards the s3 endpoint")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				s3InboxSecretKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.bucket",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Name of the bucket which serves as the inbox")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				s3InboxBucket = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.region",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "us-east-1", "Region of the s3")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				s3InboxRegion = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Ca Cert to be used for TLS traffic towards the s3 serving as the inbox")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				s3InboxCaCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "s3inbox.ready_path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path on the s3 endpoint which will be used for ready checking")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				s3InboxReadyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "server.jwt_pub_key_path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to file containing jwt key to used to verify bearer token signatures, at least one of server.jwt_pub_key_url or server.jwt_pub_key_path needs to be set")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverJwtPubKeyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "server.jwt_pub_key_url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Url to jwt key to used to verify bearer token signatures, at least one of server.jwt_pub_key_url or server.jwt_pub_key_path needs to be set")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverJwtPubKeyURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "server.key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to key file to used for TLS, both server.key and server.cert needs to be set for server to serve traffic with TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "server.cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to cert file used for TLS, both server.key and server.cert needs to be set for server to serve traffic with TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverCert = viper.GetString(flagName)
			},
		},
	)
}

func DestinationQueue() string {
	return destinationQueue
}

func S3InboxEndpoint() string {
	return s3InboxEndpoint
}
func S3InboxAccessKey() string {
	return s3InboxAccessKey
}
func S3InboxSecretKey() string {
	return s3InboxSecretKey
}
func S3InboxBucket() string {
	return s3InboxBucket
}
func S3InboxRegion() string {
	return s3InboxRegion
}
func S3InboxCaCert() string {
	return s3InboxCaCert
}
func S3InboxReadyPath() string {
	return s3InboxReadyPath
}
func ServerKey() string {
	return serverKey
}
func ServerJwtPubKeyPath() string {
	return serverJwtPubKeyPath
}
func ServerJwtPubKeyURL() string {
	return serverJwtPubKeyURL
}
func ServerCert() string {
	return serverCert
}
