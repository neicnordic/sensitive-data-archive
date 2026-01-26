package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
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

	// Check permission (skip in allow-all-data mode)
	if !config.JWTAllowAllData() {
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

	// Validate dataset access BEFORE querying for files
	// This prevents information disclosure about files in unauthorized datasets
	if !hasDatasetAccess(authCtx.Datasets, datasetID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to dataset"})

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

	// When fileId is specified, verify the file actually belongs to the specified dataset.
	// This prevents users from using dataset=X to access files from dataset=Z
	// (even if they have access to both datasets).
	if fileID != "" && file.DatasetID != datasetID {
		log.Warnf("file %s belongs to dataset %s, not requested dataset %s", file.ID, file.DatasetID, datasetID)
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found in specified dataset"})

		return
	}

	// Check permission for the file (verifies file belongs to an authorized dataset)
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
	// Use stored archive_location if available, otherwise search for it
	var location string
	if file.ArchiveLocation != "" {
		location = file.ArchiveLocation
	} else {
		// Fallback to searching for the file - this is slower and may indicate
		// missing data in the database (archive_location not populated during ingest)
		log.Warnf("file %s has no archive_location stored, falling back to FindFile search", file.ID)
		var err error
		location, err = h.storageReader.FindFile(c.Request.Context(), file.ArchivePath)
		if err != nil {
			log.Errorf("failed to find file in storage: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "file not found in storage"})

			return
		}
	}

	// Re-encrypt the header with the user's public key
	newHeader, err := h.reencryptClient.ReencryptHeader(c.Request.Context(), file.Header, publicKey)
	if err != nil {
		log.Errorf("failed to reencrypt header: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare file for download"})

		return
	}

	// Open file reader from storage (needs to support seeking to skip original header)
	fileReader, err := h.storageReader.NewFileReadSeeker(c.Request.Context(), location, file.ArchivePath)
	if err != nil {
		log.Errorf("failed to open file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})

		return
	}

	// Calculate total size: new header + body
	// Note: The archive file is header-stripped during ingest (see cmd/ingest/ingest.go).
	// The header is stored separately in the database, so:
	// - archive_file_size = body size only (encrypted data blocks, no header)
	// - file.Header = the original crypt4gh header stored separately
	// For download, we prepend the re-encrypted header to the body.
	newHeaderSize := int64(len(newHeader))
	bodySize := file.ArchiveSize // Archive is already header-stripped
	totalSize := newHeaderSize + bodySize

	// Parse Range header if present
	var rangeSpec *streaming.RangeSpec
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" {
		var rangeErr error
		rangeSpec, rangeErr = streaming.ParseRangeHeader(rangeHeader, totalSize)
		if rangeErr == streaming.ErrRangeNotSatisfiable {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			c.JSON(http.StatusRequestedRangeNotSatisfiable, gin.H{"error": "range not satisfiable"})

			return
		}
		// Other errors (nil) mean invalid format - per RFC 7233, ignore and serve full file
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
	// Note: OriginalHeaderSize is 0 because the archive file is header-stripped during ingest.
	// The body starts at offset 0 in the archive file.
	err = streaming.StreamFile(streaming.StreamConfig{
		Writer:             c.Writer,
		NewHeader:          newHeader,
		FileReader:         fileReader,
		ArchiveFileSize:    file.ArchiveSize,
		OriginalHeaderSize: 0, // Archive is header-stripped
		Range:              rangeSpec,
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
// In allow-all-data mode, all authenticated users have access to all files.
func (h *Handlers) checkFilePermission(c *gin.Context, fileID string, datasets []string) bool {
	// In allow-all-data mode, skip permission check
	if config.JWTAllowAllData() {
		return true
	}

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
