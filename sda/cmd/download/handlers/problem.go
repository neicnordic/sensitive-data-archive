package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ProblemDetails represents an RFC 9457 Problem Details response.
type ProblemDetails struct {
	Type          string `json:"type,omitempty"`
	Title         string `json:"title"`
	Status        int    `json:"status"`
	Detail        string `json:"detail,omitempty"`
	Instance      string `json:"instance,omitempty"`
	ErrorCode     string `json:"errorCode,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
}

// problemJSON sends an RFC 9457 Problem Details JSON response.
// Title is derived from the HTTP status code. CorrelationID is read from the gin context.
func problemJSON(c *gin.Context, status int, detail string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, ProblemDetails{
		Title:         http.StatusText(status),
		Status:        status,
		Detail:        detail,
		CorrelationID: c.GetString("correlationId"),
	})
}

// problemJSONWithCode sends an RFC 9457 Problem Details JSON response with an error code.
func problemJSONWithCode(c *gin.Context, status int, detail, errorCode string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, ProblemDetails{
		Title:         http.StatusText(status),
		Status:        status,
		Detail:        detail,
		ErrorCode:     errorCode,
		CorrelationID: c.GetString("correlationId"),
	})
}

// correlationIDMiddleware generates a UUID per request, stores it in the gin context
// as "correlationId", and sets the X-Correlation-ID response header.
func correlationIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()
		c.Set("correlationId", id)
		c.Header("X-Correlation-ID", id)
		c.Next()
	}
}
