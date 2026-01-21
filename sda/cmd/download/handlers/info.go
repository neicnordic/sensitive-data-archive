package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	log "github.com/sirupsen/logrus"
)

// InfoDatasets returns a list of datasets the user has access to.
// GET /info/datasets
func (h *Handlers) InfoDatasets(c *gin.Context) {
	_, ok := middleware.GetAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	// In allow-all-data mode (for testing), return all datasets
	// Otherwise, return datasets based on user's permissions
	var err error
	var datasets []database.Dataset

	if config.JWTAllowAllData() {
		datasets, err = h.db.GetAllDatasets(c.Request.Context())
	} else {
		authCtx, _ := middleware.GetAuthContext(c)
		datasets, err = h.db.GetUserDatasets(c.Request.Context(), authCtx.Datasets)
	}

	if err != nil {
		log.Errorf("failed to retrieve datasets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve datasets"})

		return
	}

	c.JSON(http.StatusOK, datasets)
}

// InfoDataset returns metadata for a specific dataset.
// GET /info/dataset?dataset=X
func (h *Handlers) InfoDataset(c *gin.Context) {
	datasetID := c.Query("dataset")
	if datasetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dataset parameter is required"})

		return
	}

	// Check user has access to this dataset
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	if !hasDatasetAccess(authCtx.Datasets, datasetID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to dataset"})

		return
	}

	info, err := h.db.GetDatasetInfo(c.Request.Context(), datasetID)
	if err != nil {
		log.Errorf("failed to retrieve dataset info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve dataset info"})

		return
	}

	if info == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "dataset not found"})

		return
	}

	c.JSON(http.StatusOK, info)
}

// InfoDatasetFiles returns a list of files in a dataset.
// GET /info/dataset/files?dataset=X
func (h *Handlers) InfoDatasetFiles(c *gin.Context) {
	datasetID := c.Query("dataset")
	if datasetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dataset parameter is required"})

		return
	}

	// Check user has access to this dataset
	authCtx, ok := middleware.GetAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}

	if !hasDatasetAccess(authCtx.Datasets, datasetID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to dataset"})

		return
	}

	files, err := h.db.GetDatasetFiles(c.Request.Context(), datasetID)
	if err != nil {
		log.Errorf("failed to retrieve dataset files: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve dataset files"})

		return
	}

	c.JSON(http.StatusOK, files)
}

// hasDatasetAccess checks if the user has access to a specific dataset.
// In allow-all-data mode, all authenticated users have access to all datasets.
func hasDatasetAccess(userDatasets []string, datasetID string) bool {
	// In allow-all-data mode, grant access to all datasets
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
