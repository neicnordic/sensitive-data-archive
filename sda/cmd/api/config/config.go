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
)

var (
	apiHost string
	apiPort int
	apiProtocol string
	RBACpath string
	RBACfile []byte
	jwtPubKeyUrl string
	jwtPubKeyPath string
	serverCert   string
	serverKey   string
	schemaPath string
	grpcHost string
	grpcPort int
	grpcTimeout int
	grpcCaCert string
	grpcClientCert string
	grpcClientKey string
)

type Grpc struct {
	CaCert      string
	ClientCert  string
	ClientKey   string
	ClientCreds credentials.TransportCredentials
	ServerCert  string
	ServerKey   string
	Host        string
	Port        int
	Timeout     int
}

func init() {
	config.RegisterFlags(
		&config.Flag{
			Name: "apiHost",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Hostname for the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "apiPort",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 0, "Port for the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "apiProtocol",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Protocol for the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				apiProtocol = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "serverCert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Server TLS certificate")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "serverKey",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "Server TLS certificate key")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				serverKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "rbacFile",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "File path to the defining Role Based Access Policies (RBAC) to the api server")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				RBACpath = viper.GetString(flagName)
				file, err := os.ReadFile(RBACpath)
				if err != nil {
					panic(fmt.Sprintf("cannot read RBAC file at %s, reason: %v", RBACpath, err))
				}
				RBACfile = file
			},
		},
		&config.Flag{
			Name: "jwtpubkeyurl",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "JWT public key URL")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyUrl = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "jwtpubkeypath",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "JWT public key path")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				jwtPubKeyPath = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpcHost",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "GRPC Host")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcHost = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpcPort",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 50051, "GRPC Port")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcPort = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "grpcTimeout",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.Int(flagName, 30, "GRPC Timeout")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcTimeout = viper.GetInt(flagName)
			},
		},
		&config.Flag{
			Name: "grpcCaCert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "GRPC Ca Certificate")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcCaCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpcClientCert",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "GRPC Client Certificate")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcClientCert = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "grpcClientKey",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "", "GRPC Client Key")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				grpcClientKey = viper.GetString(flagName)
			},
		},
		&config.Flag{
			Name: "schemaType",
			RegisterFunc: func(flagSet *pflag.FlagSet, flagName string) {
				flagSet.String(flagName, "isolated", "Path to JSON schemas to validate rabbitmq messages against")
			},
			Required: false,
			AssignFunc: func(flagName string) {
				schemaType := viper.GetString("schemaType")
				switch schemaType {
				case "federated":
					schemaPath = "/schemas/federated/"
				case "isolated":
					schemaPath = "/schemas/isolated/"
				default:
					panic(fmt.Sprintf("schema.type '%s' not supported, needs: <federated|isolated>", schemaType))
				}
			},
		},
	)
}

func ApiHost() string {
	return apiHost
}

func ApiPort() int {
	return apiPort
}

func ApiProtocl() string {
	return apiProtocol
}

func ApiAddr() string {
	return fmt.Sprintf("%s://:%s:%d", apiProtocol, apiHost, apiPort)
}

func ServerCert() string {
	return serverCert
}

func ServerKey() string {
	return serverKey
}

func SchemaPath() string {
	return schemaPath
}

func RBACPath() string {
	return RBACpath
}

func RBACFile() []byte {
	return RBACfile
}

func JwtPubKeyURL() string {
	return jwtPubKeyUrl
}

func JwtPubKeyPath() string {
	return jwtPubKeyPath
}

func SetSchemaPath(path string) {
	schemaPath = path
}

func GrpcClient() (Grpc, error) {
	var grpc Grpc
	grpc.Host = grpcHost
	grpc.Port = grpcPort
	grpc.Timeout = grpcTimeout
	if grpcClientCert != "" && grpcClientKey != "" {
		if grpcCaCert != "" {
			cacertByte, err := os.ReadFile(grpcCaCert)
			if err != nil {
				return Grpc{}, fmt.Errorf("failed to read CA certificate, reason: %v", err)
			}
			caCert := x509.NewCertPool()
			if !caCert.AppendCertsFromPEM(cacertByte) {
				return Grpc{}, fmt.Errorf("failed to append CA certificate to cert pool")
			}

			certs, err := tls.LoadX509KeyPair(viper.GetString("grpc.clientcert"), viper.GetString("grpc.clientkey"))
			if err != nil {
				return Grpc{}, fmt.Errorf("failed to load client key pair, reason: %v", err)
			}
			grpc.ClientCreds = credentials.NewTLS(
				&tls.Config{
					Certificates: []tls.Certificate{certs},
					MinVersion:   tls.VersionTLS13,
					RootCAs:      caCert,
				},
			)
		}
	} else {
			certs, err := tls.LoadX509KeyPair(viper.GetString("grpc.clientcert"), viper.GetString("grpc.clientkey"))
			if err != nil {
				return Grpc{}, fmt.Errorf("Failed to load client key pair for reencrypt")
			}

			grpc.ClientCreds = credentials.NewTLS(
				&tls.Config{
					Certificates: []tls.Certificate{certs},
					MinVersion:   tls.VersionTLS13,
				},
			)
		}
	return grpc, nil
}

func GrpcAddr() string {
	return fmt.Sprintf("%s:%d", grpcHost, grpcPort)
}
