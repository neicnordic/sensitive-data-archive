package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/streaming"
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
	hasPermission, err := h.db.CheckFilePermission(c.Request.Context(), fileID, authCtx.Datasets)
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
	file, err := h.db.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		log.Errorf("failed to retrieve file info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve file info"})

		return
	}

	if file == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})

		return
	}

	// Stream the file
	h.streamFile(c, file, publicKey)
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

	// Stream the file
	h.streamFile(c, file, publicKey)
}

// streamFile handles the actual file streaming with re-encryption.
func (h *Handlers) streamFile(c *gin.Context, file *database.File, publicKey string) {
	// Verify dependencies are available
	if h.storageReader == nil {
		log.Error("storage reader not configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage not configured"})

		return
	}

	if h.reencryptClient == nil {
		log.Error("reencrypt client not configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reencrypt service not configured"})

		return
	}

	// Verify file has required data
	if len(file.Header) == 0 {
		log.Errorf("file %s has no header", file.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file header not available"})

		return
	}

	if file.ArchivePath == "" {
		log.Errorf("file %s has no archive path", file.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file not in archive"})

		return
	}

	// Determine file location in storage
	location, err := h.storageReader.FindFile(c.Request.Context(), file.ArchivePath)
	if err != nil {
		log.Errorf("failed to find file in storage: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file not found in storage"})

		return
	}

	// Re-encrypt the header with the user's public key
	newHeader, err := h.reencryptClient.ReencryptHeader(c.Request.Context(), file.Header, publicKey)
	if err != nil {
		log.Errorf("failed to reencrypt header: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare file for download"})

		return
	}

	// Open file reader from storage
	fileReader, err := h.storageReader.NewFileReader(c.Request.Context(), location, file.ArchivePath)
	if err != nil {
		log.Errorf("failed to open file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})

		return
	}

	// Calculate total size (new header + archive file body)
	// Note: We use the archive size minus the original header size for the body
	// but for simplicity, we stream the whole archive file
	headerSize := int64(len(newHeader))
	totalSize := headerSize + file.ArchiveSize

	// Parse Range header if present
	var rangeSpec *streaming.RangeSpec
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		rangeSpec = streaming.ParseRangeHeader(rangeHeader, totalSize)
	}

	// Set response headers
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", file.ID+".c4gh"))
	c.Header("X-File-Id", file.ID)
	c.Header("X-Decrypted-Size", fmt.Sprintf("%d", file.DecryptedSize))

	if file.DecryptedChecksum != "" {
		c.Header("X-Decrypted-Checksum", file.DecryptedChecksum)
		c.Header("X-Decrypted-Checksum-Type", file.DecryptedChecksumType)
	}

	// Stream the file
	err = streaming.StreamFile(streaming.StreamConfig{
		Writer:          c.Writer,
		NewHeader:       newHeader,
		FileReader:      fileReader,
		ArchiveFileSize: file.ArchiveSize,
		Range:           rangeSpec,
	})
	if err != nil {
		log.Errorf("error streaming file: %v", err)
		// Can't send JSON error at this point, response already started

		return
	}
}

// getFileByIDOrPath retrieves a file by ID or path, sending error response if needed.
// Returns nil, nil if file not found. Returns nil, error if lookup failed.
func (h *Handlers) getFileByIDOrPath(c *gin.Context, fileID, datasetID, filePath string) (*database.File, error) {
	var file *database.File
	var err error

	if fileID != "" {
		file, err = h.db.GetFileByID(c.Request.Context(), fileID)
	} else {
		file, err = h.db.GetFileByPath(c.Request.Context(), datasetID, filePath)
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
	hasPermission, err := h.db.CheckFilePermission(c.Request.Context(), fileID, datasets)
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
