package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	log "github.com/sirupsen/logrus"
)

// DownloadByFileID handles file download by stable ID in the path.
// GET /file/{fileId}
func (h *Handlers) DownloadByFileID(c *gin.Context) {
	fileID := c.Param("fileId")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fileId is required"})

		return
	}

	// Check for required public_key header
	publicKey := c.GetHeader("public_key")
	if publicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key header is required"})

		return
	}

	// Get auth context
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	// Check permission
	hasPermission, err := database.CheckFilePermission(c.Request.Context(), fileID, authCtx.Datasets)
	if err != nil {
		log.Errorf("failed to check file permission: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check file permission"})

		return
	}

	if !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})

		return
	}

	// Get file info
	file, err := database.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		log.Errorf("failed to retrieve file info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve file info"})

		return
	}

	if file == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})

		return
	}

	// Parse Range header if present
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		// TODO: Parse RFC 7233 range header and handle partial content
		_ = rangeHeader
	}

	// TODO: Stream file content with re-encryption
	// 1. Use storage/v2 to get file reader
	// 2. Re-encrypt header via gRPC
	// 3. Stream content to client

	_ = publicKey // Will be used for re-encryption

	c.JSON(http.StatusNotImplemented, gin.H{"error": "file download not yet implemented"})
}

// DownloadByQuery handles file download by query parameters.
// GET /file?dataset=X&fileId=Y or GET /file?dataset=X&filePath=Y
func (h *Handlers) DownloadByQuery(c *gin.Context) {
	datasetID := c.Query("dataset")
	if datasetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dataset parameter is required"})

		return
	}

	fileID := c.Query("fileId")
	filePath := c.Query("filePath")

	if fileID == "" && filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "either fileId or filePath parameter is required"})

		return
	}

	if fileID != "" && filePath != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only one of fileId or filePath can be specified"})

		return
	}

	// Check for required public_key header
	publicKey := c.GetHeader("public_key")
	if publicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key header is required"})

		return
	}

	// Get auth context
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	// Get file info - either by ID or by path
	file, err := h.getFileByIDOrPath(c, fileID, datasetID, filePath)
	if err != nil {
		return // Error response already sent
	}

	if file == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})

		return
	}

	// Check permission for the file
	if !h.checkFilePermission(c, file.ID, authCtx.Datasets) {
		return // Error response already sent
	}

	// Parse Range header if present
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		// TODO: Parse RFC 7233 range header and handle partial content
		_ = rangeHeader
	}

	// TODO: Stream file content with re-encryption
	// 1. Use storage/v2 to get file reader
	// 2. Re-encrypt header via gRPC
	// 3. Stream content to client

	_ = publicKey // Will be used for re-encryption

	c.JSON(http.StatusNotImplemented, gin.H{"error": "file download not yet implemented"})
}

// getFileByIDOrPath retrieves a file by ID or path, sending error response if needed.
// Returns nil, nil if file not found. Returns nil, error if lookup failed.
func (h *Handlers) getFileByIDOrPath(c *gin.Context, fileID, datasetID, filePath string) (*database.File, error) {
	var file *database.File
	var err error

	if fileID != "" {
		file, err = database.GetFileByID(c.Request.Context(), fileID)
	} else {
		file, err = database.GetFileByPath(c.Request.Context(), datasetID, filePath)
	}

	if err != nil {
		log.Errorf("failed to retrieve file info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve file info"})

		return nil, err
	}

	return file, nil
}

// checkFilePermission checks if user has permission to access the file.
// Returns true if permission granted, false otherwise (error response already sent).
func (h *Handlers) checkFilePermission(c *gin.Context, fileID string, datasets []string) bool {
	hasPermission, err := database.CheckFilePermission(c.Request.Context(), fileID, datasets)
	if err != nil {
		log.Errorf("failed to check file permission: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check file permission"})

		return false
	}

	if !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})

		return false
	}

	return true
}
