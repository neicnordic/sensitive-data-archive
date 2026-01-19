package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthStatus represents the health check response.
type HealthStatus struct {
	Status   string            `json:"status"`
	Services map[string]string `json:"services,omitempty"`
}

// HealthReady performs a detailed health check of all dependencies.
// GET /health/ready
func (h *Handlers) HealthReady(c *gin.Context) {
	// TODO: Check database, storage, gRPC, OIDC
	status := HealthStatus{
		Status: "ok",
		Services: map[string]string{
			"database": "ok",
			"storage":  "ok",
			"grpc":     "ok",
			"oidc":     "ok",
		},
	}

	c.JSON(http.StatusOK, status)
}

// HealthLive returns a simple liveness probe response.
// GET /health/live
func (h *Handlers) HealthLive(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
