package audit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// StdoutLogger outputs audit events as JSON lines to stdout.
type StdoutLogger struct {
	encoder *json.Encoder
	mu      sync.Mutex
}

// NewStdoutLogger creates a StdoutLogger that writes to os.Stdout.
func NewStdoutLogger() *StdoutLogger {
	return newStdoutLoggerWithWriter(os.Stdout)
}

// newStdoutLoggerWithWriter creates a StdoutLogger that writes to w.
// Used in tests to capture output without redirecting os.Stdout.
func newStdoutLoggerWithWriter(w io.Writer) *StdoutLogger {
	return &StdoutLogger{
		encoder: json.NewEncoder(w),
	}
}

func (l *StdoutLogger) Log(_ context.Context, event Event) {
	// Enforce Type is always "audit"
	event.Type = "audit"
	// Always set timestamp at log time to prevent caller manipulation
	event.Timestamp = time.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.encoder.Encode(event) // best-effort, don't block HTTP response
}
