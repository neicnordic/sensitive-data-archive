package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// server struct is used to implement reencrypt.ReEncryptServer.
type server struct {
	re.UnimplementedReencryptServer
}

var Conf config.Config

// ReencryptHeader implements reencrypt.ReEncryptHeader
func (s *server) ReencryptHeader(_ context.Context, in *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	log.Debugf("Received Public key: %v", in.GetPublickey())
	log.Debugf("Received previous crypt4gh header: %v", in.GetOldheader())

	// working with the base64 encoded key as it can be sent in both HTTP headers and HTTP body
	publicKey, err := base64.StdEncoding.DecodeString(in.GetPublickey())
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(publicKey)
	newReaderPublicKey, err := keys.ReadPublicKey(reader)
	if err != nil {
		return nil, err
	}
	newReaderPublicKeyList := [][chacha20poly1305.KeySize]byte{}
	newReaderPublicKeyList = append(newReaderPublicKeyList, newReaderPublicKey)

	log.Debugf("crypt4ghkey: %v", *Conf.ReEncrypt.Crypt4GHKey)

	newheader, err := headers.ReEncryptHeader(in.GetOldheader(), *Conf.ReEncrypt.Crypt4GHKey, newReaderPublicKeyList)
	if err != nil {
		return nil, err
	}

	return &re.ReencryptResponse{Header: newheader}, nil
}

func main() {
	Conf, err := config.NewConfig("reencrypt")
	if err != nil {
		log.Fatalf("configuration loading failed, reason: %v", err)
	}

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer func() {
		if err := recover(); err != nil {
			log.Fatal("Could not recover, exiting")
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", Conf.ReEncrypt.Host, Conf.ReEncrypt.Port))
	if err != nil {
		log.Errorf("failed to listen: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	var opts []grpc.ServerOption
	if Conf.ReEncrypt.ServerCert != "" && Conf.ReEncrypt.ServerKey != "" {
		creds, err := credentials.NewServerTLSFromFile(Conf.ReEncrypt.ServerCert, Conf.ReEncrypt.ServerKey)
		if err != nil {
			log.Errorf("Failed to generate credentials: %v", err)
			sigc <- syscall.SIGINT
			panic(err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	s := grpc.NewServer(opts...)
	re.RegisterReencryptServer(s, &server{})
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Errorf("failed to serve: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}
}
