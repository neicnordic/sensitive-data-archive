package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	log "github.com/sirupsen/logrus"
)

// DrsObject represents a GA4GH DRS object response.
type DrsObject struct {
	ID            string            `json:"id"`
	SelfURI       string            `json:"self_uri"`
	Size          int64             `json:"size"`
	CreatedTime   string            `json:"created_time"`
	Checksums     []DrsChecksum     `json:"checksums"`
	AccessMethods []DrsAccessMethod `json:"access_methods"`
}

// DrsChecksum represents a checksum in a DRS object.
type DrsChecksum struct {
	Checksum string `json:"checksum"`
	Type     string `json:"type"`
}

// DrsAccessMethod represents an access method in a DRS object.
type DrsAccessMethod struct {
	Type      string       `json:"type"`
	AccessURL DrsAccessURL `json:"access_url"`
}

// DrsAccessURL represents an access URL in a DRS access method.
type DrsAccessURL struct {
	URL string `json:"url"`
}

// drsChecksumType normalises an SDA checksum type to the DRS/GA4GH lowercase form.
func drsChecksumType(sdaType string) string {
	switch strings.ToLower(sdaType) {
	case "sha256", "sha-256":
		return "sha-256"
	case "sha384", "sha-384":
		return "sha-384"
	case "sha512", "sha-512":
		return "sha-512"
	default:
		return strings.ToLower(sdaType)
	}
}

// GetDrsObject returns a GA4GH DRS object for a file identified by dataset and path.
// GET /objects/{datasetId}/{filePath}
func (h *Handlers) GetDrsObject(c *gin.Context) {
	rawPath := strings.TrimPrefix(c.Param("path"), "/")

	idx := strings.Index(rawPath, "/")
	if idx <= 0 || idx == len(rawPath)-1 {
		problemJSON(c, http.StatusBadRequest, "path must contain {datasetId}/{filePath}")

		return
	}

	datasetID := rawPath[:idx]
	filePath := rawPath[idx+1:]

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

	file, err := h.db.GetFileByPath(c.Request.Context(), datasetID, filePath)
	if err != nil {
		log.Errorf("failed to get file by path: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve file")

		return
	}

	if file == nil {
		problemJSON(c, http.StatusForbidden, "access denied")
		h.auditDenied(c)

		return
	}

	host := c.Request.Host
	scheme := "https"
	if c.Request.TLS == nil {
		scheme = "http"
	}

	// Fetch ARCHIVED checksums (over the encrypted blob, per DRS 1.5 spec)
	archivedChecksums, err := h.db.GetFileChecksums(c.Request.Context(), file.ID, "ARCHIVED")
	if err != nil {
		log.Errorf("failed to get file checksums: %v", err)
		problemJSON(c, http.StatusInternalServerError, "failed to retrieve checksums")

		return
	}

	if len(archivedChecksums) == 0 {
		log.Errorf("file %s has no ARCHIVED checksums", file.ID)
		problemJSON(c, http.StatusInternalServerError, "file has no checksums")

		return
	}

	checksums := make([]DrsChecksum, len(archivedChecksums))
	for i, ac := range archivedChecksums {
		checksums[i] = DrsChecksum{
			Checksum: ac.Checksum,
			Type:     drsChecksumType(ac.Type),
		}
	}

	obj := DrsObject{
		ID:          file.ID,
		SelfURI:     fmt.Sprintf("drs://%s/%s", host, file.ID),
		Size:        file.ArchiveSize,
		CreatedTime: file.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Checksums:   checksums,
		AccessMethods: []DrsAccessMethod{
			{
				Type: scheme,
				AccessURL: DrsAccessURL{
					URL: fmt.Sprintf("%s://%s/files/%s/content", scheme, host, file.ID),
				},
			},
		},
	}

	c.Header("Cache-Control", "private, max-age=60, must-revalidate")
	c.JSON(http.StatusOK, obj)
}
