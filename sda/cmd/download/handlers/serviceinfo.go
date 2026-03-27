package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// serviceInfoResponse represents a GA4GH Service Info response.
type serviceInfoResponse struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Type         serviceInfoType `json:"type"`
	Organization serviceInfoOrg  `json:"organization"`
	Version      string          `json:"version"`
}

type serviceInfoType struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
	Version  string `json:"version"`
}

type serviceInfoOrg struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ServiceInfo returns GA4GH service-info metadata.
// GET /service-info
func (h *Handlers) ServiceInfo(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, serviceInfoResponse{
		ID:   h.serviceID,
		Name: "SDA Download",
		Type: serviceInfoType{
			Group:    "org.ga4gh",
			Artifact: "drs",
			Version:  "1.0.0",
		},
		Organization: serviceInfoOrg{
			Name: h.serviceOrgName,
			URL:  h.serviceOrgURL,
		},
		Version: "2.0.0",
	})
}
