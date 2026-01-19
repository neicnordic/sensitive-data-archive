// Package handlers provides HTTP handlers for the download service.
package handlers

import (
	"github.com/gin-gonic/gin"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	// Add dependencies here as needed (database, storage, etc.)
}

// New creates a new Handlers instance with the given dependencies.
func New() *Handlers {
	return &Handlers{}
}

// RegisterRoutes registers all HTTP routes with the given gin engine.
func (h *Handlers) RegisterRoutes(r *gin.Engine) {
	// Health endpoints (no auth required)
	health := r.Group("/health")
	{
		health.GET("/ready", h.HealthReady)
		health.GET("/live", h.HealthLive)
	}

	// Info endpoints (auth required)
	info := r.Group("/info")
	// TODO: Add auth middleware
	{
		info.GET("/datasets", h.InfoDatasets)
		info.GET("/dataset", h.InfoDataset)
		info.GET("/dataset/files", h.InfoDatasetFiles)
	}

	// File download endpoints (auth required)
	file := r.Group("/file")
	// TODO: Add auth middleware
	{
		file.GET("/:fileId", h.DownloadByFileID)
		file.GET("", h.DownloadByQuery)
	}
}
