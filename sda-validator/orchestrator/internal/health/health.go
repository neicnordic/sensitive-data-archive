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

func Start(port int) error {
	grpcServer = grpc.NewServer()

	healthpb.RegisterHealthServer(grpcServer, healthServer)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return grpcServer.Serve(listener)
}

func SetServingStatus(status healthpb.HealthCheckResponse_ServingStatus) {
	healthServer.SetServingStatus("", status)
}

func Stop() {
	grpcServer.GracefulStop()
}
