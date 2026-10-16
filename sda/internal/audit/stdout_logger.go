package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// StdoutLogger outputs audit events as JSON lines to stdout.
type StdoutLogger struct {
	w  io.Writer
	mu sync.Mutex
}

// NewStdoutLogger creates a StdoutLogger that writes to os.Stdout.
func NewStdoutLogger() *StdoutLogger {
	return NewStdoutLoggerWithWriter(os.Stdout)
}

// NewStdoutLoggerWithWriter creates a StdoutLogger that writes to w.
// Used in tests to capture output without redirecting os.Stdout.
func NewStdoutLoggerWithWriter(w io.Writer) *StdoutLogger {
	return &StdoutLogger{w: w}
}

func (l *StdoutLogger) Log(_ context.Context, event Event) {
	// Enforce Type is always "audit"
	event.Type = "audit"
	// Always set timestamp at log time to prevent caller manipulation
	event.Timestamp = time.Now().UTC()

	// One Marshal and one Write per event, not a shared json.Encoder: an
	// Encoder keeps its first write error and drops every later event silently.
	line, err := json.Marshal(event)
	if err == nil {
		l.mu.Lock()
		_, err = l.w.Write(append(line, '\n'))
		l.mu.Unlock()
	}
	if err != nil {
		// The response is not blocked, but the record must not vanish without a trace.
		slog.Error("audit event not written", "error", err,
			"event", event.Event, "user", event.UserID, "method", event.Method, "path", event.Path, "status", event.HTTPStatus)
	}
}
