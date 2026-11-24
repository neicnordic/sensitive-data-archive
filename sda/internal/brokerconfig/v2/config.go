package brokerconfig

import (
	"github.com/neicnordic/sensitive-data-archive/pkg/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/pkg/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var brokerConfig = rabbitmq.DefaultConfig()

func init() {
	defaults := rabbitmq.DefaultConfig()
	config.RegisterFlags(
		&config.Flag{
			Name: "broker.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.Host, "Host address to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.Host = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, defaults.Port, "Port used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.Port = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "broker.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.User, "User used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.User = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.Password, "Password used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.Password = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.vhost",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.VHost, "Virtual host used to connect the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.VHost = viper.GetString(flagName)
			},
		},

		&config.Flag{
			Name: "broker.exchange",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.Exchange, "Exchange when publishing messages to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.Exchange = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ssl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, defaults.SSL, "If to connect to the rabbitmq server with tls")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.SSL = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.CACert, "File path to the ca cert file of the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.CACert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.ClientCert, "File path to the client cert file used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.ClientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, defaults.ClientKey, "File path to the client key file used to connect to the rabbitmq server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.ClientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.prefetch_count",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, defaults.PrefetchCount, "How many messages the rabbitmq server will deliver to a client before receiving delivery acknowledgements")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.PrefetchCount = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "broker.timeout",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, defaults.Timeout, "Time to wait before trying to reconnect to MQ server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.Timeout = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "broker.shutdown_grace",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Duration(flagName, defaults.ShutdownGrace, "How long a message handler that is already running gets to finish after shutdown starts, before its context is cancelled. Expects a go time.Duration parsable string")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				brokerConfig.ShutdownGrace = viper.GetDuration(flagName)
			},
		},
	)
}

func Config() rabbitmq.Config {
	return brokerConfig
}
