package main

import (
	"context"
	"fmt"
	"net"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/database"
	re "github.com/neicnordic/sda-download/internal/reencrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// server struct is used to implement reencrypt.ReEncryptServer.
type server struct {
	re.UnimplementedReencryptServer
}

// init is run before main, it sets up configuration and other required things
func init() {
	// Load configuration
	conf, err := config.NewConfig("reencrypt")
	if err != nil {
		log.Panicf("configuration loading failed, reason: %v", err)
	}
	config.Config = *conf

	// Connect to database
	db, err := database.NewDB(conf.DB)
	if err != nil {
		log.Panicf("database connection failed, reason: %v", err)
	}
	defer db.Close()
	database.DB = db

}

// ReencryptHeader implements reencrypt.ReEncryptHeader
func (s *server) ReencryptHeader(ctx context.Context, in *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	log.Debugf("Received Public key: %v", in.GetPublickey())
	log.Debugf("Received previous crypt4gh header: %v", in.GetOldheader())

	// working with the base64 data instead of the full armored key is easier
	// as it can be sent in both HTTP headers and HTTP body
	newReaderPublicKey, err := keys.ReadPublicKey(strings.NewReader("-----BEGIN CRYPT4GH PUBLIC KEY-----\n" + in.GetPublickey() + "\n-----END CRYPT4GH PUBLIC KEY-----\n"))
	if err != nil {
		return nil, err
	}

	newReaderPublicKeyList := [][chacha20poly1305.KeySize]byte{}
	newReaderPublicKeyList = append(newReaderPublicKeyList, newReaderPublicKey)

	log.Debugf("crypt4ghkey: %v", *config.Config.Grpc.Crypt4GHKey)

	newheader, err := headers.ReEncryptHeader(in.GetOldheader(), *config.Config.Grpc.Crypt4GHKey, newReaderPublicKeyList)
	if err != nil {
		return nil, err
	}

	return &re.ReencryptResponse{Header: newheader}, nil
}

func main() {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.Config.Grpc.Host, config.Config.Grpc.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	if config.Config.Grpc.ServerCert != "" && config.Config.Grpc.ServerKey != "" {
		creds, err := credentials.NewServerTLSFromFile(config.Config.Grpc.ServerCert, config.Config.Grpc.ServerKey)
		if err != nil {
			log.Fatalf("Failed to generate credentials: %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	s := grpc.NewServer(opts...)
	re.RegisterReencryptServer(s, &server{})
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
