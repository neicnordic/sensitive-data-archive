package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// HealthStatus represents the health check response.
type HealthStatus struct {
	Status   string            `json:"status"`
	Services map[string]string `json:"services,omitempty"`
}

// HealthReady performs a detailed health check of all dependencies.
// GET /health/ready
func (h *Handlers) HealthReady(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	services := make(map[string]string)
	allHealthy := true

	// Check database
	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			log.Warnf("health check: database ping failed: %v", err)
			services["database"] = "error: " + err.Error()
			allHealthy = false
		} else {
			services["database"] = "ok"
		}
	} else {
		services["database"] = "error: not configured"
		allHealthy = false
	}

	// Check storage reader
	if h.storageReader != nil {
		services["storage"] = "ok"
	} else {
		services["storage"] = "error: not configured"
		allHealthy = false
	}

	// Check gRPC reencrypt client
	if h.reencryptClient != nil {
		if err := h.reencryptClient.HealthCheck(); err != nil {
			log.Warnf("health check: grpc connection failed: %v", err)
			services["grpc"] = "error: " + err.Error()
			allHealthy = false
		} else {
			services["grpc"] = "ok"
		}
	} else {
		services["grpc"] = "error: not configured"
		allHealthy = false
	}

	status := HealthStatus{
		Services: services,
	}

	if allHealthy {
		status.Status = "ok"
		c.JSON(http.StatusOK, status)
	} else {
		status.Status = "degraded"
		c.JSON(http.StatusServiceUnavailable, status)
	}
}

// HealthLive returns a simple liveness probe response.
// GET /health/live
func (h *Handlers) HealthLive(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
