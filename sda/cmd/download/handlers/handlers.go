// Package handlers provides HTTP handlers for the download service.
package handlers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	storage "github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	db              database.Database
	storageReader   storage.Reader
	reencryptClient *reencrypt.Client
	visaValidator   *visa.Validator
	auditLogger     audit.Logger
	grpcHost        string
	grpcPort        int
}

// New creates a new Handlers instance with the given options.
func New(options ...func(*Handlers)) (*Handlers, error) {
	h := &Handlers{
		auditLogger: audit.NoopLogger{},
	}

	for _, option := range options {
		option(h)
	}

	// Validate required dependencies
	if h.db == nil {
		return nil, errors.New("database is required")
	}

	return h, nil
}

// auditDenied logs a download.denied audit event for 403 responses.
func (h *Handlers) auditDenied(c *gin.Context) {
	authCtx, _ := middleware.GetAuthContext(c)
	h.auditLogger.Log(c.Request.Context(), audit.Event{
		Event:         audit.EventDenied,
		UserID:        authCtx.Subject,
		CorrelationID: c.GetString("correlationId"),
		Path:      c.Request.URL.Path,
		HTTPStatus:    c.Writer.Status(),
	})
}

// auditFailed logs a download.failed audit event for server errors during download.
func (h *Handlers) auditFailed(c *gin.Context, authCtx middleware.AuthContext, file *database.File, reason string) {
	var fileID, datasetID string
	if file != nil {
		fileID = file.ID
		datasetID = file.DatasetID
	}

	h.auditLogger.Log(c.Request.Context(), audit.Event{
		Event:         audit.EventFailed,
		UserID:        authCtx.Subject,
		FileID:        fileID,
		DatasetID:     datasetID,
		CorrelationID: c.GetString("correlationId"),
		Path:      c.Request.URL.Path,
		HTTPStatus:    c.Writer.Status(),
		ErrorReason:   reason,
	})
}

// RegisterRoutes registers all HTTP routes with the given gin engine.
func (h *Handlers) RegisterRoutes(r *gin.Engine) {
	// Correlation ID middleware on all routes
	r.Use(correlationIDMiddleware())

	// Health endpoints (no auth required)
	health := r.Group("/health")
	{
		health.GET("/ready", h.HealthReady)
		health.GET("/live", h.HealthLive)
	}

	// Service info (no auth required)
	r.GET("/service-info", h.ServiceInfo)

	// Datasets (auth required)
	datasets := r.Group("/datasets")
	datasets.Use(middleware.TokenMiddleware(h.db, h.visaValidator, h.auditLogger))
	{
		datasets.GET("", h.ListDatasets)
		datasets.GET("/:datasetId", h.GetDataset)
		datasets.GET("/:datasetId/files", h.ListDatasetFiles)
	}

	// Files (auth required)
	files := r.Group("/files")
	files.Use(middleware.TokenMiddleware(h.db, h.visaValidator, h.auditLogger))
	{
		files.HEAD("/:fileId", h.HeadFile)
		files.GET("/:fileId", h.DownloadFile)
		files.HEAD("/:fileId/header", h.HeadFileHeader)
		files.GET("/:fileId/header", h.GetFileHeader)
		files.HEAD("/:fileId/content", h.HeadFileContent)
		files.GET("/:fileId/content", h.GetFileContent)
	}

	// DRS objects (auth required)
	objects := r.Group("/objects")
	objects.Use(middleware.TokenMiddleware(h.db, h.visaValidator, h.auditLogger))
	{
		objects.GET("/*path", h.GetDrsObject)
	}
}
