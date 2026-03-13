package handlers

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	log "github.com/sirupsen/logrus"
)

// hasDatasetAccess checks if the user has access to a specific dataset.
// In allow-all-data mode, all authenticated users have access to all datasets.
func hasDatasetAccess(userDatasets []string, datasetID string) bool {
	if config.JWTAllowAllData() {
		return true
	}

	for _, d := range userDatasets {
		if d == datasetID {
			return true
		}
	}

	return false
}

// fileInfo is the v2 API response representation of a file within a dataset.
type fileInfo struct {
	FileID        string              `json:"fileId"`
	FilePath      string              `json:"filePath"`
	Size          int64               `json:"size"`
	DecryptedSize int64               `json:"decryptedSize"`
	Checksums     []database.Checksum `json:"checksums"`
	DownloadURL   string              `json:"downloadUrl"`
}

// ListDatasets returns a paginated list of dataset IDs the user has access to.
// GET /datasets
func (h *Handlers) ListDatasets(c *gin.Context) {
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		problemJSON(c, http.StatusUnauthorized, "authentication required")

		return
	}

	var datasets []database.Dataset
	var err error

	if config.JWTAllowAllData() {
		datasets, err = h.db.GetAllDatasets(c.Request.Context())
	} else {
		datasets, err = h.db.GetUserDatasets(c.Request.Context(), authCtx.Datasets)
	}

	if err != nil {
		log.Errorf("failed to retrieve datasets: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve datasets")

		return
	}

	// Extract IDs and sort ascending
	ids := make([]string, len(datasets))
	for i, d := range datasets {
		ids[i] = d.ID
	}
	slices.Sort(ids)

	// Parse pagination parameters
	pageSize, err := parsePageSize(c)
	if err != nil {
		problemJSON(c, http.StatusBadRequest, err.Error())

		return
	}

	start := 0
	if tokenStr := c.Query("pageToken"); tokenStr != "" {
		tok, err := decodePageToken(tokenStr)
		if err != nil {
			problemJSON(c, http.StatusBadRequest, err.Error())

			return
		}

		if tok.PageSize != pageSize {
			problemJSON(c, http.StatusBadRequest, "pageSize must not change between pages")

			return
		}

		qh := queryFingerprint("datasets")
		if tok.QueryHash != qh {
			problemJSON(c, http.StatusBadRequest, "page token does not match this query")

			return
		}

		// Find cursor position: start after the cursor value
		for i, id := range ids {
			if id == tok.Cursor {
				start = i + 1

				break
			}
		}
	}

	// Slice the page
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}

	page := ids[start:end]

	// Build next page token if more items remain
	var nextPageToken *string
	if end < len(ids) {
		qh := queryFingerprint("datasets")
		tok := encodePageToken(page[len(page)-1], "", pageSize, qh)
		nextPageToken = &tok
	}

	c.Header("Cache-Control", "private, max-age=60, must-revalidate")
	c.JSON(http.StatusOK, gin.H{
		"datasets":      page,
		"nextPageToken": nextPageToken,
	})
}

// GetDataset returns metadata for a specific dataset.
// GET /datasets/:datasetId
func (h *Handlers) GetDataset(c *gin.Context) {
	datasetID := c.Param("datasetId")

	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		problemJSON(c, http.StatusUnauthorized, "authentication required")

		return
	}

	if !hasDatasetAccess(authCtx.Datasets, datasetID) {
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return
	}

	info, err := h.db.GetDatasetInfo(c.Request.Context(), datasetID)
	if err != nil {
		log.Errorf("failed to retrieve dataset info: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve dataset info")

		return
	}

	if info == nil {
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return
	}

	c.Header("Cache-Control", "private, max-age=60, must-revalidate")
	c.JSON(http.StatusOK, gin.H{
		"datasetId": info.ID,
		"date":      info.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"files":     info.FileCount,
		"size":      info.TotalSize,
	})
}

// ListDatasetFiles returns a paginated list of files in a dataset.
// GET /datasets/:datasetId/files
func (h *Handlers) ListDatasetFiles(c *gin.Context) {
	datasetID := c.Param("datasetId")

	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		problemJSON(c, http.StatusUnauthorized, "authentication required")

		return
	}

	if !hasDatasetAccess(authCtx.Datasets, datasetID) {
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return
	}

	// Check dataset exists (consistent with GetDataset — no existence leakage)
	exists, err := h.db.CheckDatasetExists(c.Request.Context(), datasetID)
	if err != nil {
		log.Errorf("failed to check dataset existence: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to check dataset")

		return
	}

	if !exists {
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return
	}

	// Validate filters
	filePath := c.Query("filePath")
	pathPrefix := c.Query("pathPrefix")

	const maxFilterLen = 4096
	if len(filePath) > maxFilterLen || len(pathPrefix) > maxFilterLen {
		problemJSON(c, http.StatusBadRequest, "filter value too long")

		return
	}

	if filePath != "" && pathPrefix != "" {
		problemJSONWithCode(c, http.StatusBadRequest,
			"filePath and pathPrefix are mutually exclusive", "FILTER_CONFLICT")

		return
	}

	pageSize, err := parsePageSize(c)
	if err != nil {
		problemJSON(c, http.StatusBadRequest, err.Error())

		return
	}

	opts := database.FileListOptions{
		FilePath:   filePath,
		PathPrefix: pathPrefix,
		Limit:      pageSize + 1,
	}

	// Build query fingerprint from stable parameters
	qh := queryFingerprint(datasetID, filePath, pathPrefix)

	if tokenStr := c.Query("pageToken"); tokenStr != "" {
		tok, err := decodePageToken(tokenStr)
		if err != nil {
			problemJSON(c, http.StatusBadRequest, err.Error())

			return
		}

		if tok.QueryHash != qh {
			problemJSON(c, http.StatusBadRequest, "page token does not match this query")

			return
		}

		opts.CursorPath = tok.Cursor
		opts.CursorID = tok.CursorID
	}

	files, err := h.db.GetDatasetFilesPaginated(c.Request.Context(), datasetID, opts)
	if err != nil {
		log.Errorf("failed to retrieve dataset files: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve dataset files")

		return
	}

	// Detect next page
	hasNext := len(files) > pageSize
	if hasNext {
		files = files[:pageSize]
	}

	// Map to v2 response format
	result := make([]fileInfo, len(files))
	for i, f := range files {
		checksums := f.Checksums
		if checksums == nil {
			checksums = []database.Checksum{}
		}
		result[i] = fileInfo{
			FileID:        f.ID,
			FilePath:      f.SubmittedPath,
			Size:          f.ArchiveSize,
			DecryptedSize: f.DecryptedSize,
			Checksums:     checksums,
			DownloadURL:   "/files/" + f.ID,
		}
	}

	var nextPageToken *string
	if hasNext {
		last := files[len(files)-1]
		tok := encodePageToken(last.SubmittedPath, last.ID, pageSize, qh)
		nextPageToken = &tok
	}

	c.Header("Cache-Control", "private, max-age=60, must-revalidate")
	c.JSON(http.StatusOK, gin.H{
		"files":         result,
		"nextPageToken": nextPageToken,
	})
}
