// Package config provides configuration for the download service.
package config

import (
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	// Server configuration
	apiHost       string
	apiPort       int
	apiServerCert string
	apiServerKey  string
	healthPort    int

	// Database configuration
	dbHost     string
	dbPort     int
	dbUser     string
	dbPassword string
	dbDatabase string
	dbSSLMode  string
	dbCACert   string

	// gRPC reencrypt service configuration
	grpcHost       string
	grpcPort       int
	grpcCACert     string
	grpcClientCert string
	grpcClientKey  string

	// OIDC configuration
	oidcIssuer      string
	oidcJWKSURL     string
	oidcAudience    string
	oidcTrustedList []string
	oidcUserinfoURL string

	// Session configuration
	sessionExpiration int
	sessionDomain     string
	sessionSecure     bool
	sessionHTTPOnly   bool
	sessionName       string
)

func init() {
	config.RegisterFlags(
		// Server flags
		&config.Flag{
			Name: "api.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "0.0.0.0", "Host address to bind the API server to")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "api.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 8080, "Port to host the API server at")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "api.server-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to the server certificate file for TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiServerCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "api.server-key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to the server key file for TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiServerKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "health.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 8081, "Port for gRPC health check server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				healthPort = viper.GetInt(flagName)
			},
		},

		// Database flags
		&config.Flag{
			Name: "db.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "localhost", "Database host")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				dbHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 5432, "Database port")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "db.user",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Database user")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				dbUser = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.password",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Database password")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				dbPassword = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.database",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "sda", "Database name")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbDatabase = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.sslmode",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "prefer", "Database SSL mode (disable, allow, prefer, require, verify-ca, verify-full)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbSSLMode = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.cacert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to CA certificate for database TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbCACert = viper.GetString(flagName)
			},
		},

		// gRPC reencrypt service flags
		&config.Flag{
			Name: "grpc.host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "gRPC reencrypt service host")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				grpcHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpc.port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 50051, "gRPC reencrypt service port")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "grpc.cacert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to CA certificate for gRPC TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcCACert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpc.client-cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client certificate for gRPC mTLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcClientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpc.client-key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client key for gRPC mTLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcClientKey = viper.GetString(flagName)
			},
		},

		// OIDC flags
		&config.Flag{
			Name: "oidc.issuer",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "OIDC issuer URL")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				oidcIssuer = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "oidc.jwks-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "OIDC JWKS URL (optional, derived from issuer if not set)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcJWKSURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "oidc.audience",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Expected audience in OIDC tokens")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcAudience = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "oidc.trusted-issuers",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.StringSlice(flagName, []string{}, "List of trusted OIDC issuers for visa validation")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcTrustedList = viper.GetStringSlice(flagName)
			},
		},
		&config.Flag{
			Name: "oidc.userinfo-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "OIDC userinfo endpoint URL (optional, derived from issuer if not set)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcUserinfoURL = viper.GetString(flagName)
			},
		},

		// Session flags
		&config.Flag{
			Name: "session.expiration",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 3600, "Session expiration time in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sessionExpiration = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "session.domain",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Cookie domain for session")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sessionDomain = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "session.secure",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "Use secure cookies (HTTPS only)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sessionSecure = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "session.http-only",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "HTTP only cookies (not accessible via JavaScript)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sessionHTTPOnly = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "session.name",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "sda_session", "Session cookie name")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				sessionName = viper.GetString(flagName)
			},
		},
	)
}

// APIHost returns the host address for the API server.
func APIHost() string {
	return apiHost
}

// APIPort returns the port for the API server.
func APIPort() int {
	return apiPort
}

// APIServerCert returns the path to the server certificate.
func APIServerCert() string {
	return apiServerCert
}

// APIServerKey returns the path to the server key.
func APIServerKey() string {
	return apiServerKey
}

// HealthPort returns the port for the gRPC health check server.
func HealthPort() int {
	return healthPort
}

// DBHost returns the database host.
func DBHost() string {
	return dbHost
}

// DBPort returns the database port.
func DBPort() int {
	return dbPort
}

// DBUser returns the database user.
func DBUser() string {
	return dbUser
}

// DBPassword returns the database password.
func DBPassword() string {
	return dbPassword
}

// DBDatabase returns the database name.
func DBDatabase() string {
	return dbDatabase
}

// DBSSLMode returns the database SSL mode.
func DBSSLMode() string {
	return dbSSLMode
}

// DBCACert returns the path to the database CA certificate.
func DBCACert() string {
	return dbCACert
}

// GRPCHost returns the gRPC service host.
func GRPCHost() string {
	return grpcHost
}

// GRPCPort returns the gRPC service port.
func GRPCPort() int {
	return grpcPort
}

// GRPCCACert returns the path to the gRPC CA certificate.
func GRPCCACert() string {
	return grpcCACert
}

// GRPCClientCert returns the path to the gRPC client certificate.
func GRPCClientCert() string {
	return grpcClientCert
}

// GRPCClientKey returns the path to the gRPC client key.
func GRPCClientKey() string {
	return grpcClientKey
}

// OIDCIssuer returns the OIDC issuer URL.
func OIDCIssuer() string {
	return oidcIssuer
}

// OIDCJWKSURL returns the OIDC JWKS URL.
func OIDCJWKSURL() string {
	return oidcJWKSURL
}

// OIDCAudience returns the expected OIDC audience.
func OIDCAudience() string {
	return oidcAudience
}

// OIDCTrustedList returns the list of trusted OIDC issuers.
func OIDCTrustedList() []string {
	return oidcTrustedList
}

// OIDCUserinfoURL returns the OIDC userinfo endpoint URL.
func OIDCUserinfoURL() string {
	return oidcUserinfoURL
}

// SessionExpiration returns the session expiration time in seconds.
func SessionExpiration() int {
	return sessionExpiration
}

// SessionDomain returns the cookie domain for sessions.
func SessionDomain() string {
	return sessionDomain
}

// SessionSecure returns whether to use secure cookies.
func SessionSecure() bool {
	return sessionSecure
}

// SessionHTTPOnly returns whether cookies are HTTP only.
func SessionHTTPOnly() bool {
	return sessionHTTPOnly
}

// SessionName returns the session cookie name.
func SessionName() string {
	return sessionName
}
