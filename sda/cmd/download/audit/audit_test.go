package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopLogger_LogDoesNotPanic(t *testing.T) {
	var logger Logger = NoopLogger{}
	assert.NotPanics(t, func() {
		logger.Log(context.Background(), Event{
			Event:         EventCompleted,
			CorrelationID: "abc-123",
			Path:          "/files/test",
			HTTPStatus:    200,
		})
	})
}

func TestStdoutLogger_OutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         EventCompleted,
		UserID:        "user@example.org",
		FileID:        "file-001",
		DatasetID:     "EGAD00000000001",
		CorrelationID: "corr-123",
		Path:          "/files/file-001",
		HTTPStatus:    200,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err, "output must be valid JSON")
	assert.Equal(t, EventCompleted, decoded.Event)
	assert.Equal(t, "user@example.org", decoded.UserID)
	assert.Equal(t, "file-001", decoded.FileID)
	assert.Equal(t, "EGAD00000000001", decoded.DatasetID)
	assert.Equal(t, "corr-123", decoded.CorrelationID)
	assert.Equal(t, "/files/file-001", decoded.Path)
	assert.Equal(t, 200, decoded.HTTPStatus)
}

func TestStdoutLogger_AlwaysSetsTypeToAudit(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Type:          "something-else",
		Event:         EventDenied,
		CorrelationID: "corr-456",
		Path:          "/files/file-002",
		HTTPStatus:    403,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.Equal(t, "audit", decoded.Type, "Type must always be 'audit' regardless of caller input")
}

func TestStdoutLogger_TimestampAlwaysOverwritten(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	// Provide a caller-supplied timestamp — logger must ignore it
	eastern := time.FixedZone("EST", -5*60*60)
	callerTS := time.Date(2025, 6, 15, 10, 30, 0, 0, eastern)

	before := time.Now().UTC()
	logger.Log(context.Background(), Event{
		Event:         EventCompleted,
		Timestamp:     callerTS,
		CorrelationID: "corr-789",
		Path:          "/files/file-003",
		HTTPStatus:    200,
	})
	after := time.Now().UTC()

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, decoded.Timestamp.Location(), "timestamp must be in UTC")
	assert.NotEqual(t, callerTS.UTC(), decoded.Timestamp, "caller-supplied timestamp must be overwritten")
	assert.True(t, !decoded.Timestamp.Before(before) && !decoded.Timestamp.After(after),
		"timestamp must be set at log time, not caller time")
}

func TestStdoutLogger_ZeroTimestampFilled(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	before := time.Now().UTC()
	logger.Log(context.Background(), Event{
		Event:         EventFailed,
		CorrelationID: "corr-000",
		Path:          "/files/file-004",
		HTTPStatus:    500,
	})
	after := time.Now().UTC()

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.False(t, decoded.Timestamp.IsZero(), "zero timestamp must be filled with current time")
	assert.True(t, !decoded.Timestamp.Before(before) && !decoded.Timestamp.After(after),
		"auto-filled timestamp must be between before and after test boundaries")
}

func TestStdoutLogger_OmitsEmptyOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         EventDenied,
		CorrelationID: "corr-omit",
		Path:          "/files/file-005",
		HTTPStatus:    401,
	})

	var raw map[string]any
	err := json.Unmarshal(buf.Bytes(), &raw)
	require.NoError(t, err)

	// Fields with omitempty should be absent when zero-valued
	assert.NotContains(t, raw, "fileId", "empty fileId should be omitted")
	assert.NotContains(t, raw, "datasetId", "empty datasetId should be omitted")
	assert.NotContains(t, raw, "bytesTransferred", "zero bytesTransferred should be omitted")
	assert.NotContains(t, raw, "authType", "empty authType should be omitted")
	assert.NotContains(t, raw, "errorReason", "empty errorReason should be omitted")

	// Required fields should always be present
	assert.Contains(t, raw, "type")
	assert.Contains(t, raw, "event")
	assert.Contains(t, raw, "timestamp")
	assert.Contains(t, raw, "correlationId")
	assert.Contains(t, raw, "path")
	assert.Contains(t, raw, "httpStatus")
}

func TestStdoutLogger_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         EventCompleted,
		CorrelationID: "corr-a",
		Path:          "/files/a",
		HTTPStatus:    200,
	})
	logger.Log(context.Background(), Event{
		Event:         EventDenied,
		CorrelationID: "corr-b",
		Path:          "/files/b",
		HTTPStatus:    403,
	})

	decoder := json.NewDecoder(&buf)

	var first Event
	require.NoError(t, decoder.Decode(&first))
	assert.Equal(t, "corr-a", first.CorrelationID)

	var second Event
	require.NoError(t, decoder.Decode(&second))
	assert.Equal(t, "corr-b", second.CorrelationID)
}

func TestAuditContract_RequiredFieldsPopulated(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         EventCompleted,
		UserID:        "user@example.org",
		CorrelationID: "corr-contract",
		Path:          "/files/file-006",
		HTTPStatus:    200,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "audit", decoded.Type, "Type must be 'audit'")
	assert.NotEmpty(t, decoded.Event, "Event must be populated")
	assert.False(t, decoded.Timestamp.IsZero(), "Timestamp must be populated")
	assert.NotEmpty(t, decoded.CorrelationID, "CorrelationID must be populated")
	assert.NotEmpty(t, decoded.Path, "Endpoint must be populated")
	assert.NotZero(t, decoded.HTTPStatus, "HTTPStatus must be populated")
}
