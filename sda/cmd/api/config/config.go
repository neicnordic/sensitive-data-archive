package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	apiHost       string
	apiPort       int
	rbacFile      string
	jwtPubKeyURL  string
	jwtPubKeyPath string
	schemaPath    string
	internalTLS   bool
	externalTLS   bool
	caCert        string
	clientKey     string
	clientCert    string
	grpcHost      string
	grpcPort      int
	grpcTimeout   int
)

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "api_host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "0.0.0.0", "Hostname for the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "api_port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 8080, "Port for the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "ca_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "TLS CA Certificate file")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				caCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "client_key",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "TLS Key file")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				clientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "client_cert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "TLS Certificate file")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				clientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "rbac_file",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "/rbac.json", "File path to the defining Role Based Access Policies (RBAC) to the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				rbacFile = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt_pub_key_url",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "http://localhost:8800/oidc/jwk", "JWT public key URL")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyURL = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwt_pub_key_path",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "JWT public key path")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "external_tls",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Toggles wheter or not to serve external HTTP endpoints using TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				externalTLS = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "internal_tls",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Bool(flagName, false, "Toggles wheter or not to serve internal GRCP communication using TLS")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				internalTLS = viper.GetBool(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt_host",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Hostname for reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "reencrypt_port",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 50051, "Port number for reencrypt service")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "grpc_timeout",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 30, "GRPC Timeout")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcTimeout = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "schema_type",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Path to JSON schemas to validate rabbitmq messages against, <federated|isolated|bigpicture>")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				schemaType := viper.GetString(flagName)
				switch schemaType {
				case "federated":
					schemaPath = "/schemas/federated/"
				case "isolated":
					schemaPath = "/schemas/isolated/"
				case "bigpicture":
					schemaPath = "/schemas/bigpicture/"
				default:
					panic(fmt.Sprintf("schema.type '%s' not supported, needs: <federated|isolated|bigpicture>", schemaType))
				}
			},
		},
	)
}

func APIHost() string {
	return apiHost
}

func APIPort() int {
	return apiPort
}

func APIAddr() string {
	return fmt.Sprintf("%s:%d", apiHost, apiPort)
}

func ExternalTLS() bool {
	return externalTLS
}

func InternalTLS() bool {
	return internalTLS
}

func ClientCert() string {
	return clientCert
}

func ClientKey() string {
	return clientKey
}

func SchemaPath() string {
	return schemaPath
}

func SetSchemaPath(path string) {
	schemaPath = path
}

func RbacFile() string {
	return rbacFile
}

func JwtPubKeyURL() string {
	return jwtPubKeyURL
}

func JwtPubKeyPath() string {
	return jwtPubKeyPath
}

func GrpcCreds() (credentials.TransportCredentials, error) {
	if !internalTLS {
		return insecure.NewCredentials(), nil
	}
	caCertBytes, err := os.ReadFile(caCert)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate from %s: %w", caCert, err)
	}

	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(caCertBytes); !ok {
		return nil, fmt.Errorf("failed to parse CA certificate PEM data from %s", caCert)
	}

	certs, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client key pair: %w", err)
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{certs},
		MinVersion:   tls.VersionTLS13,
		RootCAs:      caCertPool,
	})

	return creds, nil
}

func GrpcAddr() string {
	return fmt.Sprintf("%s:%d", grpcHost, grpcPort)
}
