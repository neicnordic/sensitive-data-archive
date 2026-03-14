package handlers

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/streaming"
	log "github.com/sirupsen/logrus"
)

// resolvedBase holds the shared file resolution state.
type resolvedBase struct {
	file     *database.File
	location string
	authCtx  middleware.AuthContext
}

// resolvedFile holds the result of resolving a file for download.
type resolvedFile struct {
	resolvedBase
	publicKey string
	newHeader []byte
	etag      string
}

// resolveFileBase performs auth, permission check, file lookup,
// and storage resolution common to both full-download and content-only endpoints.
// Returns (nil, false) if an error response was already sent.
func (h *Handlers) resolveFileBase(c *gin.Context) (*resolvedBase, bool) {
	fileID := c.Param("fileId")

	// Get auth context
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		problemJSON(c, http.StatusUnauthorized, "authentication required")

		return nil, false
	}

	// Permission check: return 403 for both "no access" AND "not found" (no existence leakage)
	if !config.JWTAllowAllData() {
		hasPermission, err := h.db.CheckFilePermission(c.Request.Context(), fileID, authCtx.Datasets)
		if err != nil {
			log.Errorf("failed to check file permission: %v", err)
			problemJSON(c, http.StatusInternalServerError, "failed to check file permission")

			return nil, false
		}

		if !hasPermission {
			problemJSON(c, http.StatusForbidden, "access denied")
			h.auditDenied(c)

			return nil, false
		}
	}

	// Get file from DB
	file, err := h.db.GetFileByID(c.Request.Context(), fileID)
	if err != nil {
		log.Errorf("failed to retrieve file info: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve file info")

		return nil, false
	}

	if file == nil {
		// Return 403 (not 404) to avoid leaking file existence
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return nil, false
	}

	if file.ArchivePath == "" {
		log.Errorf("file %s has no archive path", file.ID)
		problemJSON(c, http.StatusInternalServerError, "file not in archive")

		return nil, false
	}

	// Determine storage location
	var location string
	if file.ArchiveLocation != "" {
		location = file.ArchiveLocation
	} else {
		if h.storageReader == nil {
			log.Error("storage reader not configured")
			problemJSON(c, http.StatusInternalServerError, "storage not configured")

			return nil, false
		}

		log.Warnf("file %s has no archive_location stored, falling back to FindFile search", file.ID)

		location, err = h.storageReader.FindFile(c.Request.Context(), file.ArchivePath)
		if err != nil {
			log.Errorf("failed to find file in storage: %v", err)
			problemJSON(c, http.StatusInternalServerError, "file not found in storage")

			return nil, false
		}
	}

	return &resolvedBase{
		file:     file,
		location: location,
		authCtx:  authCtx,
	}, true
}

// resolveFileForDownload wraps resolveFileBase, extracts a public key,
// and invokes gRPC re-encryption.
func (h *Handlers) resolveFileForDownload(c *gin.Context) (*resolvedFile, bool) {
	// Extract public key from headers
	publicKey, errorCode, detail := extractPublicKey(c)
	if errorCode != "" {
		problemJSONWithCode(c, http.StatusBadRequest, detail, errorCode)

		return nil, false
	}

	base, ok := h.resolveFileBase(c)
	if !ok {
		return nil, false
	}

	// Validate file has header
	if len(base.file.Header) == 0 {
		log.Errorf("file %s has no header", base.file.ID)
		problemJSON(c, http.StatusInternalServerError, "file header not available")

		return nil, false
	}

	// Re-encrypt header
	if h.reencryptClient == nil {
		log.Error("reencrypt client not configured")
		problemJSON(c, http.StatusInternalServerError, "reencrypt service not configured")

		return nil, false
	}

	newHeader, err := h.reencryptClient.ReencryptHeader(c.Request.Context(), base.file.Header, publicKey)
	if err != nil {
		log.Errorf("failed to reencrypt header: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to prepare file for download")

		return nil, false
	}

	// ETag from re-encrypted header (full SHA-256)
	hash := sha256.Sum256(newHeader)
	etag := fmt.Sprintf(`"%x"`, hash[:])

	return &resolvedFile{
		resolvedBase: *base,
		publicKey:    publicKey,
		newHeader:    newHeader,
		etag:         etag,
	}, true
}

// contentDisposition returns the Content-Disposition header value for a file download.
// Appends .c4gh extension if not already present.
func contentDisposition(submittedPath string) string {
	filename := filepath.Base(submittedPath)
	if !strings.HasSuffix(filename, ".c4gh") {
		filename += ".c4gh"
	}

	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}

// DownloadFile handles file download by stable ID.
// GET /files/:fileId
func (h *Handlers) DownloadFile(c *gin.Context) {
	resolved, ok := h.resolveFileForDownload(c)
	if !ok {
		return
	}

	file := resolved.file

	// Open file reader from storage
	if h.storageReader == nil {
		log.Error("storage reader not configured")
		problemJSON(c, http.StatusInternalServerError, "storage not configured")
		h.auditFailed(c, resolved.authCtx, file, "storage not configured")

		return
	}

	fileReader, err := h.storageReader.NewFileReadSeeker(c.Request.Context(), resolved.location, file.ArchivePath)
	if err != nil {
		log.Errorf("failed to open file: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to open file")
		h.auditFailed(c, resolved.authCtx, file, "failed to open file")

		return
	}

	// Calculate total size
	newHeaderSize := int64(len(resolved.newHeader))
	totalSize := newHeaderSize + file.ArchiveSize

	// If-Range check
	honorRange := streaming.CheckIfRange(c.GetHeader("If-Range"), resolved.etag, time.Time{})

	// Parse Range header
	var rangeSpec *streaming.RangeSpec
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" && honorRange {
		var rangeErr error
		rangeSpec, rangeErr = streaming.ParseRangeHeader(rangeHeader, totalSize)
		if errors.Is(rangeErr, streaming.ErrRangeInvalid) {
			fileReader.Close()
			problemJSONWithCode(c, http.StatusBadRequest, "invalid range header", "RANGE_INVALID")

			return
		}
		if errors.Is(rangeErr, streaming.ErrRangeNotSatisfiable) {
			fileReader.Close()
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			problemJSON(c, http.StatusRequestedRangeNotSatisfiable, "range not satisfiable")

			return
		}
	}

	// Set response headers
	c.Header("Accept-Ranges", "bytes")
	c.Header("ETag", resolved.etag)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDisposition(file.SubmittedPath))
	c.Header("Cache-Control", "private, max-age=60, must-revalidate")

	// Stream the file
	err = streaming.StreamFile(streaming.StreamConfig{
		Writer:             c.Writer,
		NewHeader:          resolved.newHeader,
		FileReader:         fileReader,
		ArchiveFileSize:    file.ArchiveSize,
		OriginalHeaderSize: 0,
		Range:              rangeSpec,
	})
	if err != nil {
		log.Errorf("error streaming file: %v", err)
		h.auditFailed(c, resolved.authCtx, file, "streaming error")

		return
	}

	// Audit event on completion
	h.auditLogger.Log(c.Request.Context(), audit.Event{
		Event:         "download.completed",
		UserID:        resolved.authCtx.Subject,
		FileID:        file.ID,
		DatasetID:     file.DatasetID,
		CorrelationID: c.GetString("correlationId"),
		Endpoint:      c.Request.URL.Path,
		HTTPStatus:    c.Writer.Status(),
	})
}

// HeadFile handles HEAD requests for file metadata.
// HEAD /files/:fileId
func (h *Handlers) HeadFile(c *gin.Context) {
	resolved, ok := h.resolveFileForDownload(c)
	if !ok {
		return
	}

	file := resolved.file

	// Calculate total size: re-encrypted header + archive body
	totalSize := int64(len(resolved.newHeader)) + file.ArchiveSize

	// Set response headers
	c.Header("Content-Length", fmt.Sprintf("%d", totalSize))
	c.Header("ETag", resolved.etag)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", contentDisposition(file.SubmittedPath))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "private, max-age=60, must-revalidate")

	c.Status(http.StatusOK)
}

// resolvedContentFile holds the result of resolving a file for body-only content access.
type resolvedContentFile struct {
	resolvedBase
	etag string
}

// contentETag computes a stable ETag from file ID and archive size.
// This is recipient-independent (no re-encrypted header involved).
func contentETag(fileID string, archiveSize int64) string {
	hash := sha256.Sum256([]byte(fileID + ":" + strconv.FormatInt(archiveSize, 10)))

	return fmt.Sprintf(`"%x"`, hash[:])
}

// resolveFileForContent wraps resolveFileBase and computes the basic content ETag.
func (h *Handlers) resolveFileForContent(c *gin.Context) (*resolvedContentFile, bool) {
	base, ok := h.resolveFileBase(c)
	if !ok {
		return nil, false
	}

	etag := contentETag(base.file.ID, base.file.ArchiveSize)

	return &resolvedContentFile{
		resolvedBase: *base,
		etag:         etag,
	}, true
}

// GetFileHeader handles requests for the re-encrypted file header only.
// GET /files/:fileId/header
func (h *Handlers) GetFileHeader(c *gin.Context) {
	resolved, ok := h.resolveFileForDownload(c)
	if !ok {
		return
	}

	file := resolved.file

	// Stable content ETag (same value /content would return)
	cETag := contentETag(file.ID, file.ArchiveSize)

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", len(resolved.newHeader)))
	c.Header("SDA-Content-ETag", cETag)
	c.Data(http.StatusOK, "application/octet-stream", resolved.newHeader)

	h.auditLogger.Log(c.Request.Context(), audit.Event{
		Event:         "download.header",
		UserID:        resolved.authCtx.Subject,
		FileID:        file.ID,
		DatasetID:     file.DatasetID,
		CorrelationID: c.GetString("correlationId"),
		Endpoint:      c.Request.URL.Path,
		HTTPStatus:    c.Writer.Status(),
	})
}

// HeadFileHeader handles HEAD requests for the file header metadata.
// HEAD /files/:fileId/header
func (h *Handlers) HeadFileHeader(c *gin.Context) {
	resolved, ok := h.resolveFileForDownload(c)
	if !ok {
		return
	}

	file := resolved.file
	cETag := contentETag(file.ID, file.ArchiveSize)

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", len(resolved.newHeader)))
	c.Header("SDA-Content-ETag", cETag)
	c.Status(http.StatusOK)
}

// GetFileContent handles requests for the file body (archive data without header).
// GET /files/:fileId/content
func (h *Handlers) GetFileContent(c *gin.Context) {
	resolved, ok := h.resolveFileForContent(c)
	if !ok {
		return
	}

	file := resolved.file

	// Open file reader from storage
	if h.storageReader == nil {
		log.Error("storage reader not configured")
		problemJSON(c, http.StatusInternalServerError, "storage not configured")
		h.auditFailed(c, resolved.authCtx, file, "storage not configured")

		return
	}

	fileReader, err := h.storageReader.NewFileReadSeeker(c.Request.Context(), resolved.location, file.ArchivePath)
	if err != nil {
		log.Errorf("failed to open file: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to open file")
		h.auditFailed(c, resolved.authCtx, file, "failed to open file")

		return
	}

	// If-Range check
	honorRange := streaming.CheckIfRange(c.GetHeader("If-Range"), resolved.etag, time.Time{})

	// Parse Range header
	var rangeSpec *streaming.RangeSpec
	rangeHeader := c.GetHeader("Range")
	if rangeHeader != "" && honorRange {
		var rangeErr error
		rangeSpec, rangeErr = streaming.ParseRangeHeader(rangeHeader, file.ArchiveSize)
		if errors.Is(rangeErr, streaming.ErrRangeInvalid) {
			fileReader.Close()
			problemJSONWithCode(c, http.StatusBadRequest, "invalid range header", "RANGE_INVALID")

			return
		}
		if errors.Is(rangeErr, streaming.ErrRangeNotSatisfiable) {
			fileReader.Close()
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", file.ArchiveSize))
			problemJSON(c, http.StatusRequestedRangeNotSatisfiable, "range not satisfiable")

			return
		}
	}

	// Set response headers
	c.Header("Accept-Ranges", "bytes")
	c.Header("ETag", resolved.etag)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "private, max-age=60, must-revalidate")

	// Stream the body only
	err = streaming.StreamBodyOnly(streaming.StreamBodyConfig{
		Writer:          c.Writer,
		FileReader:      fileReader,
		ArchiveFileSize: file.ArchiveSize,
		Range:           rangeSpec,
	})
	if err != nil {
		log.Errorf("error streaming file content: %v", err)
		h.auditFailed(c, resolved.authCtx, file, "streaming error")

		return
	}

	// Audit event on completion
	h.auditLogger.Log(c.Request.Context(), audit.Event{
		Event:         "download.content",
		UserID:        resolved.authCtx.Subject,
		FileID:        file.ID,
		DatasetID:     file.DatasetID,
		CorrelationID: c.GetString("correlationId"),
		Endpoint:      c.Request.URL.Path,
		HTTPStatus:    c.Writer.Status(),
	})
}

// HeadFileContent handles HEAD requests for the file content metadata.
// HEAD /files/:fileId/content
func (h *Handlers) HeadFileContent(c *gin.Context) {
	resolved, ok := h.resolveFileForContent(c)
	if !ok {
		return
	}

	file := resolved.file

	c.Header("Content-Length", fmt.Sprintf("%d", file.ArchiveSize))
	c.Header("ETag", resolved.etag)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "private, max-age=60, must-revalidate")
	c.Status(http.StatusOK)
}
