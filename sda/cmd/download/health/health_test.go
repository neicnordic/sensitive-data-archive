package health

import (
	"testing"

	"github.com/stretchr/testify/assert"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestServingStatusConstants(t *testing.T) {
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, Serving)
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, NotServing)
}

func TestSetServingStatus(t *testing.T) {
	// Test that SetServingStatus doesn't panic
	assert.NotPanics(t, func() {
		SetServingStatus(Serving)
	})

	assert.NotPanics(t, func() {
		SetServingStatus(NotServing)
	})
}

func TestStop_WhenNotStarted(t *testing.T) {
	// Test that Stop doesn't panic when server hasn't been started
	assert.NotPanics(t, func() {
		Stop()
	})
}
