package reencrypt

import (
	"context"
	"fmt"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/config"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CallReencryptHeader re-encrypts the header of a file using the public key
// provided and returns the new header. The function uses gRPC to
// communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate
// is provided for secure communication.
func CallReencryptHeader(oldHeader []byte, c4ghPubKey string, grpcConf config.Grpc) ([]byte, error) {
	var opts []grpc.DialOption
	switch {
	case grpcConf.ClientCreds != nil:
		opts = append(opts, grpc.WithTransportCredentials(grpcConf.ClientCreds))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", grpcConf.Host, grpcConf.Port), opts...)
	if err != nil {
		log.Errorf("failed to open a new gRPC channel, reason: %v", err)

		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(grpcConf.Timeout)*time.Second)
	defer cancel()

	c := NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &ReencryptRequest{Oldheader: oldHeader, Publickey: c4ghPubKey})
	if err != nil {
		log.Errorf("failed to connect to the reencrypt service, reason %v", err)

		return nil, err
	}

	return res.Header, nil
}
