package audit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// failOnceWriter fails its first Write and passes the rest through.
type failOnceWriter struct {
	buf    bytes.Buffer
	failed bool
}

func (w *failOnceWriter) Write(p []byte) (int, error) {
	if !w.failed {
		w.failed = true

		return 0, errors.New("broken pipe")
	}

	return w.buf.Write(p)
}

func TestStdoutLogger_WriteErrorDoesNotSilenceLaterEvents(t *testing.T) {
	w := &failOnceWriter{}
	l := NewStdoutLoggerWithWriter(w)

	l.Log(context.Background(), Event{Event: EventAPIRequest, UserID: "first"})
	l.Log(context.Background(), Event{Event: EventAPIRequest, UserID: "second"})

	out := w.buf.String()
	if strings.Contains(out, `"userId":"first"`) {
		t.Fatalf("first event should have failed to write, got %q", out)
	}
	if !strings.Contains(out, `"userId":"second"`) || !strings.Contains(out, `"type":"audit"`) {
		t.Fatalf("second event not written after a failed write, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected exactly one line, got %q", out)
	}
}
