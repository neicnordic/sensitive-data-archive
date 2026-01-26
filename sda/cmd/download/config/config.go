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

	// Storage configuration
	storageBackend string

	// gRPC reencrypt service configuration
	grpcHost       string
	grpcPort       int
	grpcTimeout    int
	grpcCACert     string
	grpcClientCert string
	grpcClientKey  string

	// JWT configuration (for token validation)
	jwtPubKeyPath   string
	jwtPubKeyURL    string
	jwtAllowAllData bool // For testing: allow authenticated users access to all datasets

	// OIDC configuration (optional, for discovery)
	oidcIssuer   string
	oidcAudience string

	// Session configuration
	sessionExpiration int
	sessionDomain     string
	sessionSecure     bool
	sessionHTTPOnly   bool
	sessionName       string

	// Cache configuration
	cacheEnabled       bool
	cacheFileTTL       int
	cachePermissionTTL int
	cacheDatasetTTL    int
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

		// Storage flags
		&config.Flag{
			Name: "storage.backend",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "archive", "Storage backend name (used for storage/v2 configuration)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				storageBackend = viper.GetString(flagName)
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
			Name: "grpc.timeout",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 10, "gRPC request timeout in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcTimeout = viper.GetInt(flagName)
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

		// JWT flags (for token validation - at least one of path or url must be set)
		&config.Flag{
			Name: "jwt.pubkey-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to directory containing JWT public key files (PEM format)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt.pubkey-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "URL to JWKS endpoint for JWT verification")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt.allow-all-data",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Allow authenticated users access to all datasets (for testing only)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtAllowAllData = viper.GetBool(flagName)
			},
		},

		// OIDC flags (optional, for token validation settings)
		&config.Flag{
			Name: "oidc.issuer",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Expected OIDC issuer in tokens (optional)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcIssuer = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "oidc.audience",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Expected audience in tokens (optional)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				oidcAudience = viper.GetString(flagName)
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

		// Cache flags
		&config.Flag{
			Name: "cache.enabled",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "Enable database query caching")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				cacheEnabled = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "cache.file-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 300, "TTL for file query cache in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				cacheFileTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "cache.permission-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 120, "TTL for permission check cache in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				cachePermissionTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "cache.dataset-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 300, "TTL for dataset query cache in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				cacheDatasetTTL = viper.GetInt(flagName)
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

// StorageBackend returns the storage backend name.
func StorageBackend() string {
	return storageBackend
}

// GRPCHost returns the gRPC service host.
func GRPCHost() string {
	return grpcHost
}

// GRPCPort returns the gRPC service port.
func GRPCPort() int {
	return grpcPort
}

// GRPCTimeout returns the gRPC request timeout in seconds.
func GRPCTimeout() int {
	return grpcTimeout
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

// JWTPubKeyPath returns the path to the JWT public key directory.
func JWTPubKeyPath() string {
	return jwtPubKeyPath
}

// JWTPubKeyURL returns the JWKS URL for JWT verification.
func JWTPubKeyURL() string {
	return jwtPubKeyURL
}

// JWTAllowAllData returns whether authenticated users have access to all datasets.
// This should only be used for testing purposes.
func JWTAllowAllData() bool {
	return jwtAllowAllData
}

// OIDCIssuer returns the expected OIDC issuer.
func OIDCIssuer() string {
	return oidcIssuer
}

// OIDCAudience returns the expected OIDC audience.
func OIDCAudience() string {
	return oidcAudience
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

// CacheEnabled returns whether database query caching is enabled.
func CacheEnabled() bool {
	return cacheEnabled
}

// CacheFileTTL returns the TTL for file query cache in seconds.
func CacheFileTTL() int {
	return cacheFileTTL
}

// CachePermissionTTL returns the TTL for permission check cache in seconds.
func CachePermissionTTL() int {
	return cachePermissionTTL
}

// CacheDatasetTTL returns the TTL for dataset query cache in seconds.
func CacheDatasetTTL() int {
	return cacheDatasetTTL
}
