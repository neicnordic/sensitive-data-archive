package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/pkg/observability"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
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
func (s *server) ReencryptHeader(ctx context.Context, in *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	_, span := observability.StartSpan(ctx, "ReencryptHeader")
	defer span.End()

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

	var newReaderPublicKey [chacha20poly1305.KeySize]byte
	if len(publicKey) == chacha20poly1305.KeySize {
		// Raw 32-byte X25519 key — use directly
		copy(newReaderPublicKey[:], publicKey)
	} else {
		// Legacy: PEM text — pass to crypt4gh key parser
		reader := bytes.NewReader(publicKey)
		parsedKey, err := keys.ReadPublicKey(reader)
		if err != nil {
			return nil, status.Error(400, err.Error())
		}
		newReaderPublicKey = parsedKey
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

// recoveryUnaryInterceptor turns a panic in a gRPC handler into an Internal
// error instead of letting it crash the server. ReEncryptHeader parses a
// crypt4gh header supplied by the caller, and a malformed header packet makes
// the crypt4gh reader panic; without this the whole reencrypt service goes down.
func recoveryUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("recovered from panic in %s: %v", info.FullMethod, r)
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()

	return handler(ctx, req)
}

// newReencryptServer builds the gRPC server with the recovery interceptor
// attached. main and the wiring test both build the server through it; the test
// pins that this function installs the interceptor.
func newReencryptServer(opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts, grpc.ChainUnaryInterceptor(recoveryUnaryInterceptor))

	return grpc.NewServer(opts...)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	conf, err := config.NewConfig("reencrypt")
	if err != nil {
		return fmt.Errorf("configuration loading failed, reason: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-reencrypt")
	if err != nil {
		return fmt.Errorf("failed to setup OTel SDK: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shutdown OTel SDK", "err", err)
		}
		shutdownCancel()
	}()

	ctx, startupSpan := observability.StartSpan(ctx, "start up")
	defer startupSpan.End()

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
				return fmt.Errorf("failed to read CA certificate: %v", err)
			}

			caCert = x509.NewCertPool()
			if !caCert.AppendCertsFromPEM(caFile) {
				return errors.New("failed to append ca certificate")
			}

			serverCert, err = tls.LoadX509KeyPair(conf.ReEncrypt.ServerCert, conf.ReEncrypt.ServerKey)
			if err != nil {
				return fmt.Errorf("failed to parse certificates: %v", err)
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
				return fmt.Errorf("failed to generate tlsConfig: %v", err)
			}
			opts = []grpc.ServerOption{grpc.Creds(creds)}
		}
	}

	opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	s := newReencryptServer(opts...)

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
		return fmt.Errorf("health server failed to listen: %v", err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		log.Debugf("health server listening at %v", healthServerListener.Addr())
		if err := p.Serve(healthServerListener); err != nil {
			slog.Error("health server failed to serve", slog.Any("error", err))
			sigc <- syscall.SIGINT
		}
	}()
	defer func() {
		if err := healthServerListener.Close(); err != nil {
			slog.Error("health server failed to close", slog.Any("error", err))
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", conf.ReEncrypt.Host, conf.ReEncrypt.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	defer func() {
		if err := lis.Close(); err != nil {
			slog.Error("reencrypt listener failed to close", slog.Any("error", err))
		}
	}()

	// Start reencrypt server
	log.Printf("reencrypt server listening at %v", lis.Addr())
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Errorf("reencrypt server failed to serve: %v", err)
			sigc <- syscall.SIGINT
		}
	}()

	defer s.GracefulStop()

	startupSpan.End()
	sig := <-sigc
	slog.Info("received signal, shutting down gracefully", "signal", sig)
	cancel()

	return nil
}
