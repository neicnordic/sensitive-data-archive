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

	// Service info
	serviceID      string
	serviceOrgName string
	serviceOrgURL  string

	// Database configuration
	dbHost       string
	dbPort       int
	dbUser       string
	dbPassword   string
	dbDatabase   string
	dbSSLMode    string
	dbCACert     string
	dbClientCert string
	dbClientKey  string

	// Storage configuration

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

	// Permission model configuration
	permissionModel string // "ownership" | "visa" | "combined"

	// Auth configuration
	authAllowOpaque bool // Allow userinfo-based auth for opaque tokens

	// Visa (GA4GH) configuration
	visaEnabled          bool
	visaSource           string // "userinfo" | "token"
	visaUserinfoURL      string
	visaTrustedIssuers   string // Path to JSON file with trusted issuer+JKU pairs
	visaDatasetIDMode    string // "raw" | "suffix"
	visaIdentityMode     string // "broker-bound" | "strict-sub" | "strict-iss-sub"
	visaValidateAsserted bool
	visaMaxVisas         int
	visaMaxJWKSPerReq    int
	visaMaxVisaSize      int
	visaCacheTokenTTL    int
	visaCacheJWKTTL      int
	visaCacheValidTTL    int
	visaCacheUserinfoTTL int
	visaAllowInsecureJKU bool

	// Application environment
	appEnvironment string

	// Audit configuration
	auditRequired bool

	// Pagination configuration
	paginationHMACSecret string
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
		// Service info flags
		&config.Flag{
			Name: "service.id",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "neicnordic.sda.download", "GA4GH service-info ID (reverse domain notation)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serviceID = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "service.org-name",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Organization name for service-info")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				serviceOrgName = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "service.org-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Organization URL for service-info")
			},
			Required: true,
			AssignFunc: func(flagName string) {
				serviceOrgURL = viper.GetString(flagName)
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

		&config.Flag{
			Name: "db.clientcert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client certificate for database mTLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbClientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "db.clientkey",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to client key for database mTLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				dbClientKey = viper.GetString(flagName)
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

		// Permission model flag
		&config.Flag{
			Name: "permission.model",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "combined", "Permission model: ownership, visa, or combined")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				permissionModel = viper.GetString(flagName)
			},
		},

		// Auth flags
		&config.Flag{
			Name: "auth.allow-opaque",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "Allow userinfo-based auth for opaque (non-JWT) tokens")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				authAllowOpaque = viper.GetBool(flagName)
			},
		},

		// Visa (GA4GH) flags
		&config.Flag{
			Name: "visa.enabled",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Enable GA4GH visa support for dataset access")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaEnabled = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "visa.source",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "userinfo", "Visa source: userinfo (GA4GH compliant) or token (legacy)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaSource = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "visa.userinfo-url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Userinfo endpoint URL (optional, discovered from oidc.issuer if not set)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaUserinfoURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "visa.trusted-issuers-path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Path to JSON file with trusted issuer+JKU pairs")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaTrustedIssuers = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "visa.dataset-id-mode",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "raw", "Dataset ID extraction mode: raw (exact visa value) or suffix (last URL/URN segment)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaDatasetIDMode = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "visa.identity.mode",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "broker-bound", "Identity binding mode: broker-bound, strict-sub, or strict-iss-sub")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaIdentityMode = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "visa.validate-asserted",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, true, "Reject visas where asserted timestamp is in the future")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaValidateAsserted = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "visa.limits.max-visas",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 200, "Maximum number of visas to process per passport")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaMaxVisas = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.limits.max-jwks-per-request",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 10, "Maximum distinct JKU fetches per single request")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaMaxJWKSPerReq = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.limits.max-visa-size",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 16384, "Maximum visa JWT size in bytes")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaMaxVisaSize = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.cache.token-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 3600, "Token-keyed cache TTL in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaCacheTokenTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.cache.jwk-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 300, "JWK cache TTL in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaCacheJWKTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.cache.validation-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 120, "Visa validation cache TTL in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaCacheValidTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.cache.userinfo-ttl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 60, "Userinfo cache TTL in seconds")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaCacheUserinfoTTL = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "visa.allow-insecure-jku",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Allow HTTP (non-TLS) JKU URLs in trusted issuers (for testing only)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				visaAllowInsecureJKU = viper.GetBool(flagName)
			},
		},

		// Application environment flag
		&config.Flag{
			Name: "app.environment",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Application environment (set to 'production' to enable production guards)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				appEnvironment = viper.GetString(flagName)
			},
		},

		// Audit flags
		&config.Flag{
			Name: "audit.required",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Require a real audit logger (fail startup if noop)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				auditRequired = viper.GetBool(flagName)
			},
		},

		// Pagination flags
		&config.Flag{
			Name: "pagination.hmac-secret",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "HMAC secret for signing page tokens (must be same across replicas)")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				paginationHMACSecret = viper.GetString(flagName)
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

// ServiceID returns the GA4GH service-info ID.
func ServiceID() string {
	return serviceID
}

// ServiceOrgName returns the organization name for service-info.
func ServiceOrgName() string {
	return serviceOrgName
}

// ServiceOrgURL returns the organization URL for service-info.
func ServiceOrgURL() string {
	return serviceOrgURL
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

// DBClientCert returns the path to the client certificate for database mTLS.
func DBClientCert() string {
	return dbClientCert
}

// DBClientKey returns the path to the client key for database mTLS.
func DBClientKey() string {
	return dbClientKey
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

// PermissionModel returns the permission model (ownership, visa, or combined).
func PermissionModel() string {
	return permissionModel
}

// AuthAllowOpaque returns whether opaque (non-JWT) tokens are allowed via userinfo.
func AuthAllowOpaque() bool {
	return authAllowOpaque
}

// VisaEnabled returns whether GA4GH visa support is enabled.
func VisaEnabled() bool {
	return visaEnabled
}

// VisaSource returns the visa source (userinfo or token).
func VisaSource() string {
	return visaSource
}

// VisaUserinfoURL returns the configured userinfo endpoint URL.
func VisaUserinfoURL() string {
	return visaUserinfoURL
}

// VisaTrustedIssuersPath returns the path to the trusted issuers JSON file.
func VisaTrustedIssuersPath() string {
	return visaTrustedIssuers
}

// VisaDatasetIDMode returns the dataset ID extraction mode (raw or suffix).
func VisaDatasetIDMode() string {
	return visaDatasetIDMode
}

// VisaIdentityMode returns the identity binding mode.
func VisaIdentityMode() string {
	return visaIdentityMode
}

// VisaValidateAsserted returns whether to reject visas with future asserted timestamps.
func VisaValidateAsserted() bool {
	return visaValidateAsserted
}

// VisaMaxVisas returns the maximum number of visas to process per passport.
func VisaMaxVisas() int {
	return visaMaxVisas
}

// VisaMaxJWKSPerRequest returns the maximum distinct JKU fetches per request.
func VisaMaxJWKSPerRequest() int {
	return visaMaxJWKSPerReq
}

// VisaMaxVisaSize returns the maximum visa JWT size in bytes.
func VisaMaxVisaSize() int {
	return visaMaxVisaSize
}

// VisaCacheTokenTTL returns the token-keyed cache TTL in seconds.
func VisaCacheTokenTTL() int {
	return visaCacheTokenTTL
}

// VisaCacheJWKTTL returns the JWK cache TTL in seconds.
func VisaCacheJWKTTL() int {
	return visaCacheJWKTTL
}

// VisaCacheValidationTTL returns the visa validation cache TTL in seconds.
func VisaCacheValidationTTL() int {
	return visaCacheValidTTL
}

// VisaCacheUserinfoTTL returns the userinfo cache TTL in seconds.
func VisaCacheUserinfoTTL() int {
	return visaCacheUserinfoTTL
}

// VisaAllowInsecureJKU returns whether HTTP (non-TLS) JKU URLs are allowed.
// This should only be used for testing purposes.
func VisaAllowInsecureJKU() bool {
	return visaAllowInsecureJKU
}

// Environment returns the application environment (e.g. "production", "staging").
func Environment() string {
	return appEnvironment
}

// AuditRequired returns whether audit logging is required.
func AuditRequired() bool {
	return auditRequired
}

// PaginationHMACSecret returns the HMAC secret for signing pagination tokens.
func PaginationHMACSecret() string {
	return paginationHMACSecret
}
