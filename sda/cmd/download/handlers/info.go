package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
)

// InfoDatasets returns a list of datasets the user has access to.
// GET /info/datasets
func (h *Handlers) InfoDatasets(c *gin.Context) {
	// TODO: Extract visas from authenticated user context
	visas := []string{} // Placeholder

	datasets, err := database.GetUserDatasets(c.Request.Context(), visas)
	if err != nil {
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

	// TODO: Check user has access to this dataset

	info, err := database.GetDatasetInfo(c.Request.Context(), datasetID)
	if err != nil {
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

	// TODO: Check user has access to this dataset

	files, err := database.GetDatasetFiles(c.Request.Context(), datasetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve dataset files"})

		return
	}

	c.JSON(http.StatusOK, files)
}
