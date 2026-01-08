package broker

import (
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// amqpConfig stores configuration for rabbitmq broker
type amqpConfig struct {
	host          string
	port          int
	user          string
	password      string
	exchange      string
	vhost         string
	ssl           bool
	verifyPeer    bool
	cACert        string
	clientCert    string
	clientKey     string
	serverName    string
	prefetchCount int
}

var globalConf *amqpConfig

func init() {
	globalConf = &amqpConfig{}

	config.RegisterFlags(
		&config.Flag{
			Name: "broker.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The host the broker is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.host = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 0, "The port the broker is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.port = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "broker.exchange",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The exchange the client will use when publishing messages")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.exchange = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Username to used to authenticate with in communication with broker")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.user = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Password to used to authenticate with in communication with broker")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.password = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.vhost",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The virtual host name to connect to")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.vhost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ssl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "If the broker connection should use ssl")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.ssl = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "broker.verify-peer",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "If the broker connection should use verify-peer, if true client cert, and client key needs to be provided")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.verifyPeer = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "broker.ca-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The broker ca cert")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.cACert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The cert the client will use in communication with the broker")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.client-key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The key for the client cert the client will use in communication with the broker")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.server-name",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "ServerName is used to verify the hostname on the returned certificates if ssl is enabled")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.serverName = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "broker.prefetch-count",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 2, "How many messages the broker will try to keep on the network for the consumers before receiving delivery acks")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.prefetchCount = viper.GetInt(flagName)
			},
		},
	)
}

// buildMQURI builds the MQ connection URI
func (c *amqpConfig) buildMQURI() string {
	protocol := "amqp"
	if c.ssl {
		protocol = "amqps"
	}

	return fmt.Sprintf("%s://%s:%s@%s:%d%s", protocol, c.user, c.password, c.host, c.port, c.vhost)
}

func Host(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.host = v
	}
}
func Port(v int) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.port = v
	}
}
func Exchange(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.exchange = v
	}
}
func User(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.user = v
	}
}
func Password(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.password = v
	}
}
func Vhost(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.vhost = v
	}
}
func Ssl(v bool) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.ssl = v
	}
}
func VerifyPeer(v bool) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.verifyPeer = v
	}
}
func CACert(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.cACert = v
	}
}
func ClientCert(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.clientCert = v
	}
}
func ClientKey(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.clientKey = v
	}
}
func ServerName(v string) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.serverName = v
	}
}
func PrefetchCount(v int) func(c *amqpConfig) {
	return func(c *amqpConfig) {
		c.prefetchCount = v
	}
}

func (c *amqpConfig) clone() *amqpConfig {
	return &amqpConfig{
		host:          c.host,
		port:          c.port,
		exchange:      c.exchange,
		user:          c.user,
		password:      c.password,
		vhost:         c.vhost,
		ssl:           c.ssl,
		verifyPeer:    c.verifyPeer,
		cACert:        c.cACert,
		clientCert:    c.clientCert,
		clientKey:     c.clientKey,
		serverName:    c.serverName,
		prefetchCount: c.prefetchCount,
	}
}
