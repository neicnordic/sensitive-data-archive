package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
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

	// TODO: Extract visas from authenticated user context
	visas := []string{} // Placeholder

	// Check permission
	hasPermission, err := database.CheckFilePermission(c.Request.Context(), fileID, visas)
	if err != nil {
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

	// TODO: Extract visas from authenticated user context
	visas := []string{} // Placeholder

	var file *database.File
	var err error

	if fileID != "" {
		// Check permission
		hasPermission, permErr := database.CheckFilePermission(c.Request.Context(), fileID, visas)
		if permErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check file permission"})

			return
		}

		if !hasPermission {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})

			return
		}

		file, err = database.GetFileByID(c.Request.Context(), fileID)
	} else {
		// Lookup by path
		file, err = database.GetFileByPath(c.Request.Context(), datasetID, filePath)
		if file != nil {
			// Check permission for the found file
			hasPermission, permErr := database.CheckFilePermission(c.Request.Context(), file.ID, visas)
			if permErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check file permission"})

				return
			}

			if !hasPermission {
				c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})

				return
			}
		}
	}

	if err != nil {
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

	c.JSON(http.StatusNotImplemented, gin.H{"error": "file download not yet implemented"})
}
