package reencrypt

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

func TestNewClient(t *testing.T) {
	client := NewClient("localhost", 50051)

	assert.NotNil(t, client)
	assert.Equal(t, "localhost", client.host)
	assert.Equal(t, 50051, client.port)
	assert.Equal(t, 10*time.Second, client.timeout) // default
}

func TestNewClient_WithTimeout(t *testing.T) {
	client := NewClient("localhost", 50051, WithTimeout(30*time.Second))

	assert.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.timeout)
}

func TestNewClient_WithTLS(t *testing.T) {
	client := NewClient("localhost", 50051, WithTLS("/path/ca.crt", "/path/client.crt", "/path/client.key"))

	assert.NotNil(t, client)
	assert.Equal(t, "/path/ca.crt", client.caCert)
	assert.Equal(t, "/path/client.crt", client.clientCert)
	assert.Equal(t, "/path/client.key", client.clientKey)
}

func TestClient_Close_WhenNotConnected(t *testing.T) {
	client := NewClient("localhost", 50051)

	// Should not error when closing without connecting
	err := client.Close()
	assert.NoError(t, err)
}

func TestHealthCheck_Serving(t *testing.T) {
	// Start a gRPC server with health service
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
	healthgrpc.RegisterHealthServer(srv, healthSrv)

	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	client := NewClient("localhost", port)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestHealthCheck_NotServing(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", healthgrpc.HealthCheckResponse_NOT_SERVING)
	healthgrpc.RegisterHealthServer(srv, healthSrv)

	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	client := NewClient("localhost", port)
	defer client.Close()

	err = client.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_SERVING")
}

func TestHealthCheck_ServerDown(t *testing.T) {
	// Use a port with nothing listening
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port
	lis.Close()

	client := NewClient("localhost", port)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.HealthCheck(ctx)
	assert.Error(t, err)
}

// Note: Integration tests for ReencryptHeader would require a running gRPC server
// These are better suited as integration tests in a test environment
