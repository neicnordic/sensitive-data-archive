// Package audit provides audit logging for the download service.
// It defines the Logger interface and event types used to record
// download operations for compliance and monitoring.
package audit

import (
	"context"
	"time"
)

// Event represents an audit event for download operations.
type Event struct {
	Type             string    `json:"type"`  // routing tag: always "audit"
	Event            string    `json:"event"` // "download.completed", "download.denied", "download.failed"
	Timestamp        time.Time `json:"timestamp"`
	UserID           string    `json:"userId"`
	FileID           string    `json:"fileId,omitempty"`
	DatasetID        string    `json:"datasetId,omitempty"`
	CorrelationID    string    `json:"correlationId"`
	Endpoint         string    `json:"endpoint"`
	HTTPStatus       int       `json:"httpStatus"`
	BytesTransferred int64     `json:"bytesTransferred,omitempty"`
	AuthType         string    `json:"authType,omitempty"`
	ErrorReason      string    `json:"errorReason,omitempty"`
}

// Logger defines the interface for audit logging.
type Logger interface {
	Log(ctx context.Context, event Event)
}

// NoopLogger discards all audit events. Used when audit logging is not required.
type NoopLogger struct{}

func (NoopLogger) Log(context.Context, Event) {}
