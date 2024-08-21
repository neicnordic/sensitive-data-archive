package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// server struct is used to implement reencrypt.ReEncryptServer.
type server struct {
	re.UnimplementedReencryptServer
	c4ghPrivateKey *[32]byte
}

// ReencryptHeader implements reencrypt.ReEncryptHeader
// called with a crypt4gh header and a public key along with an optional dataeditlist,
// returns a new crypt4gh header using the same symmetric key as the original header
// but encrypted with the new public key. If a dataeditlist is provided and contains at
// least one entry it is added to the new header, replacing any existing dataeditlist. If
// no dataeditlist is passed and one exists already, it is kept in the new header.
func (s *server) ReencryptHeader(_ context.Context, in *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	log.Debugf("Received Public key: %v", in.GetPublickey())
	log.Debugf("Received previous crypt4gh header: %v", in.GetOldheader())

	// working with the base64 encoded key as it can be sent in both HTTP headers and HTTP body
	publicKey, err := base64.StdEncoding.DecodeString(in.GetPublickey())
	if err != nil {
		return nil, status.Error(400, err.Error())
	}

	if h := in.GetOldheader(); h == nil {
		return nil, status.Error(400, "no header received")
	}

	extraHeaderPackets := make([]headers.EncryptedHeaderPacket, 0)
	dataEditList := in.GetDataeditlist()

	if len(dataEditList) > 0 { // linter doesn't like checking for nil before len

		// Check that G115: integer overflow conversion int -> uint32 is satisfied
		if len(dataEditList) > int(^uint32(0)) {
			return nil, status.Error(400, "data edit list too long")
		}

		// Only do this if we're passed a data edit whose length fits in a uint32
		dataEditListPacket := headers.DataEditListHeaderPacket{
			PacketType:    headers.PacketType{PacketType: headers.DataEditList},
			NumberLengths: uint32(len(dataEditList)), //nolint:gosec // we're checking the length above
			Lengths:       dataEditList,
		}
		extraHeaderPackets = append(extraHeaderPackets, dataEditListPacket)
	}

	reader := bytes.NewReader(publicKey)
	newReaderPublicKey, err := keys.ReadPublicKey(reader)
	if err != nil {
		return nil, status.Error(400, err.Error())
	}
	newReaderPublicKeyList := [][chacha20poly1305.KeySize]byte{}
	newReaderPublicKeyList = append(newReaderPublicKeyList, newReaderPublicKey)

	log.Debugf("crypt4ghkey: %v", *s.c4ghPrivateKey)

	newheader, err := headers.ReEncryptHeader(in.GetOldheader(), *s.c4ghPrivateKey, newReaderPublicKeyList, extraHeaderPackets...)
	if err != nil {
		return nil, status.Error(400, err.Error())
	}

	return &re.ReencryptResponse{Header: newheader}, nil
}

func main() {
	conf, err := config.NewConfig("reencrypt")
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

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", conf.ReEncrypt.Host, conf.ReEncrypt.Port))
	if err != nil {
		log.Errorf("failed to listen: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	var opts []grpc.ServerOption
	if conf.ReEncrypt.ServerCert != "" && conf.ReEncrypt.ServerKey != "" {
		switch {
		case conf.ReEncrypt.CACert != "":
			caFile, err := os.ReadFile(conf.ReEncrypt.CACert)
			if err != nil {
				log.Errorf("Failed to read CA certificate: %v", err)
				sigc <- syscall.SIGINT
				panic(err)
			}

			caCert := x509.NewCertPool()
			if !caCert.AppendCertsFromPEM(caFile) {
				sigc <- syscall.SIGINT
				panic("Failed to append ca certificate")
			}

			serverCert, err := tls.LoadX509KeyPair(conf.ReEncrypt.ServerCert, conf.ReEncrypt.ServerKey)
			if err != nil {
				log.Errorf("Failed to parse certificates: %v", err)
				sigc <- syscall.SIGINT
				panic(err)
			}

			creds := credentials.NewTLS(
				&tls.Config{
					Certificates: []tls.Certificate{serverCert},
					ClientAuth:   tls.RequireAndVerifyClientCert,
					MinVersion:   tls.VersionTLS13,
					ClientCAs:    caCert,
				},
			)
			opts = []grpc.ServerOption{grpc.Creds(creds)}
		default:
			creds, err := credentials.NewServerTLSFromFile(conf.ReEncrypt.ServerCert, conf.ReEncrypt.ServerKey)
			if err != nil {
				log.Errorf("Failed to generate tlsConfig: %v", err)
				sigc <- syscall.SIGINT
				panic(err)
			}
			opts = []grpc.ServerOption{grpc.Creds(creds)}
		}
	}

	s := grpc.NewServer(opts...)
	re.RegisterReencryptServer(s, &server{c4ghPrivateKey: conf.ReEncrypt.Crypt4GHKey})
	reflection.Register(s)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Errorf("failed to serve: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}
}
