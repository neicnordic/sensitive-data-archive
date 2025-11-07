package reencrypt

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"google.golang.org/grpc"
)

type mockServer struct {
	UnimplementedReencryptServer
	headerResponse []byte
}

func (s *mockServer) ReencryptHeader(ctx context.Context, req *ReencryptRequest) (*ReencryptResponse, error) {
	return &ReencryptResponse{Header: s.headerResponse}, nil
}

func TestCallReencryptHeader(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	srv := grpc.NewServer()
	mockHeader := []byte("mocked-header")
	RegisterReencryptServer(srv, &mockServer{headerResponse: mockHeader})

	go func() {
		_ = srv.Serve(lis)
	}()
	defer srv.GracefulStop()

	host, portStr, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	grpcConf := config.Grpc{
		Host:    host,
		Port:    port,
		Timeout: 2,
	}
	oldHeader := []byte("old-header")
	pubKey := "test-pubkey"
	res, err := CallReencryptHeader(oldHeader, pubKey, grpcConf)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(res) != string(mockHeader) {
		t.Errorf("expected header %q, got %q", mockHeader, res)
	}
}
