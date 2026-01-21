// Package handlers provides HTTP handlers for the download service.
package handlers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	storage "github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	db              database.Database
	storageReader   storage.Reader
	reencryptClient *reencrypt.Client
	grpcHost        string
	grpcPort        int
}

// New creates a new Handlers instance with the given options.
func New(options ...func(*Handlers)) (*Handlers, error) {
	h := &Handlers{}

	for _, option := range options {
		option(h)
	}

	// Validate required dependencies
	if h.db == nil {
		return nil, errors.New("database is required")
	}

	return h, nil
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
	info.Use(middleware.TokenMiddleware(h.db))
	{
		info.GET("/datasets", h.InfoDatasets)
		info.GET("/dataset", h.InfoDataset)
		info.GET("/dataset/files", h.InfoDatasetFiles)
	}

	// File download endpoints (auth required)
	file := r.Group("/file")
	file.Use(middleware.TokenMiddleware(h.db))
	{
		file.GET("/:fileId", h.DownloadByFileID)
		file.GET("", h.DownloadByQuery)
	}
}
