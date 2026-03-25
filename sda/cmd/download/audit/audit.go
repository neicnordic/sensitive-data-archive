// Package audit provides audit logging for the download service.
// It defines the Logger interface and event types used to record
// download operations for compliance and monitoring.
package audit

import (
	"context"
	"time"
)

// EventName is a typed string for audit event names.
type EventName string

const (
	EventCompleted EventName = "download.completed"
	EventDenied    EventName = "download.denied"
	EventFailed    EventName = "download.failed"
	EventContent   EventName = "download.content"
	EventHeader    EventName = "download.header"
)

// Event represents an audit event for download operations.
type Event struct {
	Type             string    `json:"type"`  // routing tag: always "audit"
	Event            EventName `json:"event"`
	Timestamp        time.Time `json:"timestamp"`
	UserID           string    `json:"userId"`
	FileID           string    `json:"fileId,omitempty"`
	DatasetID        string    `json:"datasetId,omitempty"`
	CorrelationID    string    `json:"correlationId"`
	Path             string    `json:"path"`
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
