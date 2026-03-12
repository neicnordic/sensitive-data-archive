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
			Event:         "download.completed",
			CorrelationID: "abc-123",
			Endpoint:      "/files/test",
			HTTPStatus:    200,
		})
	})
}

func TestStdoutLogger_OutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         "download.completed",
		UserID:        "user@example.org",
		FileID:        "file-001",
		DatasetID:     "EGAD00000000001",
		CorrelationID: "corr-123",
		Endpoint:      "/files/file-001",
		HTTPStatus:    200,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err, "output must be valid JSON")
	assert.Equal(t, "download.completed", decoded.Event)
	assert.Equal(t, "user@example.org", decoded.UserID)
	assert.Equal(t, "file-001", decoded.FileID)
	assert.Equal(t, "EGAD00000000001", decoded.DatasetID)
	assert.Equal(t, "corr-123", decoded.CorrelationID)
	assert.Equal(t, "/files/file-001", decoded.Endpoint)
	assert.Equal(t, 200, decoded.HTTPStatus)
}

func TestStdoutLogger_AlwaysSetsTypeToAudit(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Type:          "something-else",
		Event:         "download.denied",
		CorrelationID: "corr-456",
		Endpoint:      "/files/file-002",
		HTTPStatus:    403,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.Equal(t, "audit", decoded.Type, "Type must always be 'audit' regardless of caller input")
}

func TestStdoutLogger_TimestampIsUTC(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	// Provide a non-UTC timestamp
	eastern := time.FixedZone("EST", -5*60*60)
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, eastern)

	logger.Log(context.Background(), Event{
		Event:         "download.completed",
		Timestamp:     ts,
		CorrelationID: "corr-789",
		Endpoint:      "/files/file-003",
		HTTPStatus:    200,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, decoded.Timestamp.Location(), "timestamp must be in UTC")
	assert.Equal(t, ts.UTC(), decoded.Timestamp)
}

func TestStdoutLogger_ZeroTimestampFilled(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	before := time.Now().UTC()
	logger.Log(context.Background(), Event{
		Event:         "download.failed",
		CorrelationID: "corr-000",
		Endpoint:      "/files/file-004",
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
		Event:         "download.denied",
		CorrelationID: "corr-omit",
		Endpoint:      "/files/file-005",
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
	assert.Contains(t, raw, "endpoint")
	assert.Contains(t, raw, "httpStatus")
}

func TestStdoutLogger_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer
	logger := newStdoutLoggerWithWriter(&buf)

	logger.Log(context.Background(), Event{
		Event:         "download.completed",
		CorrelationID: "corr-a",
		Endpoint:      "/files/a",
		HTTPStatus:    200,
	})
	logger.Log(context.Background(), Event{
		Event:         "download.denied",
		CorrelationID: "corr-b",
		Endpoint:      "/files/b",
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
		Event:         "download.completed",
		UserID:        "user@example.org",
		CorrelationID: "corr-contract",
		Endpoint:      "/files/file-006",
		HTTPStatus:    200,
	})

	var decoded Event
	err := json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "audit", decoded.Type, "Type must be 'audit'")
	assert.NotEmpty(t, decoded.Event, "Event must be populated")
	assert.False(t, decoded.Timestamp.IsZero(), "Timestamp must be populated")
	assert.NotEmpty(t, decoded.CorrelationID, "CorrelationID must be populated")
	assert.NotEmpty(t, decoded.Endpoint, "Endpoint must be populated")
	assert.NotZero(t, decoded.HTTPStatus, "HTTPStatus must be populated")
}
