// Package audit provides audit logging for the api and download services.
// It defines the Logger interface and event types used to record
// operations for compliance and monitoring.
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

	EventAPIRequest EventName = "api.request"
)

// Event represents an audit event for api and download operations.
type Event struct {
	Type             string    `json:"type"` // routing tag: always "audit"
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
	Method           string    `json:"method,omitempty"`
	FilePath         string    `json:"filePath,omitempty"`
	SubmissionUser   string    `json:"submissionUser,omitempty"`
	AccessionID      string    `json:"accessionId,omitempty"`
}

// Logger defines the interface for audit logging.
type Logger interface {
	Log(ctx context.Context, event Event)
}

// NoopLogger discards all audit events. Used when audit logging is not required.
type NoopLogger struct{}

func (NoopLogger) Log(context.Context, Event) {}

type eventKey struct{}

// NewContext returns a copy of ctx that carries ev, so handlers can add
// details to the event before it is logged.
func NewContext(ctx context.Context, ev *Event) context.Context {
	return context.WithValue(ctx, eventKey{}, ev)
}

// FromContext returns the event stored in ctx, or a throwaway event if there
// is none, so callers can set fields without a nil check.
func FromContext(ctx context.Context) *Event {
	if ev, ok := ctx.Value(eventKey{}).(*Event); ok {
		return ev
	}

	return &Event{}
}
