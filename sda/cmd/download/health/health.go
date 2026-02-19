// Package health provides gRPC health check server for Kubernetes probes.
package health

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var healthServer *health.Server
var grpcServer *grpc.Server

func init() {
	healthServer = health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
}

// Start starts the gRPC health check server on the specified port.
func Start(port int) error {
	grpcServer = grpc.NewServer()

	healthpb.RegisterHealthServer(grpcServer, healthServer)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	return grpcServer.Serve(listener)
}

// SetServingStatus sets the serving status for the health server.
func SetServingStatus(status healthpb.HealthCheckResponse_ServingStatus) {
	healthServer.SetServingStatus("", status)
}

// Stop gracefully stops the gRPC health server.
func Stop() {
	if grpcServer != nil {
		grpcServer.GracefulStop()
	}
}

// ServingStatus constants for convenience.
const (
	Serving    = healthpb.HealthCheckResponse_SERVING
	NotServing = healthpb.HealthCheckResponse_NOT_SERVING
)
