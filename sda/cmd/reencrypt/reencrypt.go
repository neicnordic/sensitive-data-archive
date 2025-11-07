package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// server struct is used to implement reencrypt.ReEncryptServer.
type server struct {
	re.UnimplementedReencryptServer
	c4ghPrivateKeyList []*[32]byte
}

// hServer struct is used to implement the proxy grpc health.HealthServer.
type hServer struct {
	healthgrpc.UnimplementedHealthServer
	srvCert   tls.Certificate
	srvCACert *x509.CertPool
	srvPort   int
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
		if len(dataEditList) > int(math.MaxUint32) {
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

	for _, key := range s.c4ghPrivateKeyList {
		newheader, err := headers.ReEncryptHeader(in.GetOldheader(), *key, newReaderPublicKeyList, extraHeaderPackets...)
		if err == nil {
			return &re.ReencryptResponse{Header: newheader}, nil
		}
	}

	return nil, status.Error(400, "header reencryption failed, no matching key available")
}

// Check implements the healthgrpc.HealthServer Check method for the proxy grpc Health server.
// This method probes internally the health of reencrypt's server and returns the service or
// server status. The corresponding grpc health server serves as a proxy to the internal health
// service of the reencrypt server so that k8s grpc probes can be used when TLS is enabled.
func (p *hServer) Check(ctx context.Context, in *healthgrpc.HealthCheckRequest) (*healthgrpc.HealthCheckResponse, error) {
	rpcCtx, rpcCancel := context.WithTimeout(ctx, time.Second*2)
	defer rpcCancel()

	var opts []grpc.DialOption
	if p.srvCert.Certificate != nil {
		creds := credentials.NewTLS(
			&tls.Config{
				Certificates: []tls.Certificate{p.srvCert},
				MinVersion:   tls.VersionTLS13,
				RootCAs:      p.srvCACert,
			},
		)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", "127.0.0.1", p.srvPort), opts...)
	if err != nil {
		log.Printf("failed to dial: %v", err)

		return nil, status.Error(codes.NotFound, "unknown service")
	}
	defer conn.Close()

	resp, err := healthgrpc.NewHealthClient(conn).Check(rpcCtx,
		&healthgrpc.HealthCheckRequest{
			Service: in.Service})
	if err != nil {
		log.Printf("failed to check: %v", err)

		return nil, status.Error(codes.NotFound, "unknown service")
	}

	if resp.GetStatus() != healthgrpc.HealthCheckResponse_SERVING {
		log.Debugf("service unhealthy (responded with %q)", resp.GetStatus().String())
	}

	return &healthgrpc.HealthCheckResponse{
		Status: resp.GetStatus(),
	}, nil
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

	var (
		opts       []grpc.ServerOption
		serverCert tls.Certificate
		caCert     *x509.CertPool
	)
	if conf.ReEncrypt.ServerCert != "" && conf.ReEncrypt.ServerKey != "" {
		switch {
		case conf.ReEncrypt.CACert != "":
			caFile, err := os.ReadFile(conf.ReEncrypt.CACert)
			if err != nil {
				log.Errorf("Failed to read CA certificate: %v", err)
				sigc <- syscall.SIGINT
				panic(err)
			}

			caCert = x509.NewCertPool()
			if !caCert.AppendCertsFromPEM(caFile) {
				sigc <- syscall.SIGINT
				panic("Failed to append ca certificate")
			}

			serverCert, err = tls.LoadX509KeyPair(conf.ReEncrypt.ServerCert, conf.ReEncrypt.ServerKey)
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
	re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: conf.ReEncrypt.C4ghPrivateKeyList})
	reflection.Register(s)

	// Add health service for reencrypt server
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(re.Reencrypt_ServiceDesc.ServiceName, healthgrpc.HealthCheckResponse_SERVING)
	healthgrpc.RegisterHealthServer(s, healthServer)

	// Start proxy health server
	p := grpc.NewServer()
	healthgrpc.RegisterHealthServer(p, &hServer{srvCert: serverCert, srvCACert: caCert, srvPort: conf.ReEncrypt.Port})

	healthServerListener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", conf.ReEncrypt.Host, conf.ReEncrypt.Port+1))
	if err != nil {
		log.Errorf("failed to listen: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	go func() {
		log.Debugf("health server listening at %v", healthServerListener.Addr())
		if err := p.Serve(healthServerListener); err != nil {
			log.Errorf("failed to serve: %v", err)
			sigc <- syscall.SIGINT
			panic(err)
		}
	}()

	// Start reencrypt server
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Errorf("failed to serve: %v", err)
		sigc <- syscall.SIGINT
		panic(err)
	}
}
