package postgres

import (
	"fmt"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type dbConfig struct {
	host                  string
	port                  int
	user                  string
	password              string
	databaseName          string
	schema                string
	cACert                string
	sslMode               string
	clientCert            string
	clientKey             string
	maxIdleConnections    int
	maxOpenConnections    int
	connectionMaxIdleTime time.Duration
	connectionMaxLifeTime time.Duration
}

// Initialize globalConf with default values
var globalConf = &dbConfig{
	host:                  "", // No default, needs to be provided by config / NewPostgresSQLDatabase options
	port:                  0,  // No default, needs to be provided by config / NewPostgresSQLDatabase options
	user:                  "", // No default, needs to be provided by config / NewPostgresSQLDatabase options
	password:              "", // No default, needs to be provided by config / NewPostgresSQLDatabase options
	databaseName:          "sda",
	schema:                "sda",
	cACert:                "",
	sslMode:               "disable",
	clientCert:            "",
	clientKey:             "",
	maxIdleConnections:    2,
	maxOpenConnections:    0,
	connectionMaxIdleTime: 0,
	connectionMaxLifeTime: 0,
}

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "database.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.host, "The host the postgres database is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.host = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, globalConf.port, "The port the database is served on")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.port = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "database.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.user, "Username to used to authenticate with in communication with database")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.user = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.password, "Password to used to authenticate with in communication with database")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				globalConf.password = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.ssl_mode",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.sslMode, "The database ssl mode")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.sslMode = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.cACert, "The database ca cert")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.cACert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.client_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.clientCert, "The cert the client will use in communication with the database")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.client_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.clientKey, "The key for the client cert the client will use in communication with the database")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.clientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.name",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.databaseName, "Database to connect to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.databaseName = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.schema",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, globalConf.schema, "Database schema to use as search path")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.schema = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "database.max_idle_connections",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, globalConf.maxIdleConnections, "Sets the maximum number of connections in the idle connection pool, set to <= 0 for unlimited")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.maxIdleConnections = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "database.max_open_connections",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, globalConf.maxOpenConnections, "Sets the maximum number of open connections to the database, set to <= 0 for unlimited")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.maxOpenConnections = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "database.connection_max_idle_time",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Duration(flagName, globalConf.connectionMaxIdleTime, "Sets the maximum amount of time a connection may be idle, set to <= 0 for unlimited. Expects a go time.Duration parsable string")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.connectionMaxIdleTime = viper.GetDuration(flagName)
			},
		},
		&config.Flag{
			Name: "database.connection_max_life_time",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Duration(flagName, globalConf.connectionMaxLifeTime, "Sets the maximum amount of time a connection may be reused, set to <= 0 for unlimited. Expects a go time.Duration parsable string")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				globalConf.connectionMaxLifeTime = viper.GetDuration(flagName)
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

func MaxIdleConnections(v int) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.maxIdleConnections = v
	}
}
func MaxOpenConnections(v int) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.maxOpenConnections = v
	}
}
func ConnectionMaxIdleTime(v time.Duration) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.connectionMaxIdleTime = v
	}
}
func ConnectionMaxLifeTime(v time.Duration) func(c *dbConfig) {
	return func(c *dbConfig) {
		c.connectionMaxLifeTime = v
	}
}

func (c *dbConfig) clone() *dbConfig {
	return &dbConfig{
		host:                  c.host,
		port:                  c.port,
		user:                  c.user,
		password:              c.password,
		databaseName:          c.databaseName,
		schema:                c.schema,
		cACert:                c.cACert,
		sslMode:               c.sslMode,
		clientCert:            c.clientCert,
		clientKey:             c.clientKey,
		maxIdleConnections:    c.maxIdleConnections,
		maxOpenConnections:    c.maxOpenConnections,
		connectionMaxIdleTime: c.connectionMaxIdleTime,
		connectionMaxLifeTime: c.connectionMaxLifeTime,
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
