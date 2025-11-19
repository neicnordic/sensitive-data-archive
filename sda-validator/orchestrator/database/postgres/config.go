package postgres

import (
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// dbConfig stores configuration for rabbitmq broker
type dbConfig struct {
	host         string
	port         int
	user         string
	password     string
	databaseName string
	schema       string
	cACert       string
	sslMode      string
	clientCert   string
	clientKey    string
}

var globalConf *dbConfig

func init() {
	globalConf = &dbConfig{}

	config.RegisterFlags(
		&config.Flag{
			Name: "database.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The host the postgres database is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.host = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 0, "The port the database is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.port = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "database.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Username to used to authenticate with in communication with database")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.user = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Password to used to authenticate with in communication with database")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.password = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.ssl-mode",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "disable", "The database ssl mode")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.sslMode = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.ca-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The database ca cert")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.cACert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.client-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The cert the client will use in communication with the database")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.client-key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "The key for the client cert the client will use in communication with the database")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.name",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "sda_validator_orchestrator", "Database to connect to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.databaseName = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.schema",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "sda_validator_orchestrator", "Database schema to use as search path")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.schema = viper.GetString(flagName)
			},
		},
	)
}

func Host(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.host = v
	}
}
func Port(v int) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.port = v
	}
}
func User(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.user = v
	}
}
func Password(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.password = v
	}
}
func SslMode(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.sslMode = v
	}
}
func CACert(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.cACert = v
	}
}
func ClientCert(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.clientCert = v
	}
}
func ClientKey(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.clientKey = v
	}
}
func DatabaseName(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.databaseName = v
	}
}
func Schema(v string) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.schema = v
	}
}

func (c *dbConfig) clone() *dbConfig {
	return &dbConfig{
		host:         c.host,
		port:         c.port,
		user:         c.user,
		password:     c.password,
		databaseName: c.databaseName,
		schema:       c.schema,
		cACert:       c.cACert,
		sslMode:      c.sslMode,
		clientCert:   c.clientCert,
		clientKey:    c.clientKey,
	}
}

// dataSourceName builds a postgresql data source string to use with sql.Open().
func (c *dbConfig) dataSourceName() string {
	connInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s search_path=%s",
		c.host, c.port, c.user, c.password, c.databaseName, c.sslMode, c.schema)

	if c.sslMode == "disable" {
		return connInfo
	}

	if c.cACert != "" {
		connInfo = fmt.Sprintf("%s sslrootcert=%s", connInfo, c.cACert)
	}

	if c.clientCert != "" {
		connInfo = fmt.Sprintf("%s sslcert=%s", connInfo, c.clientCert)
	}

	if c.clientKey != "" {
		connInfo = fmt.Sprintf("%s sslkey=%s", connInfo, c.clientKey)
	}

	return connInfo
}
