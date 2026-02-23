package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	SetPaginationSecret([]byte("test-secret-key-for-pagination"))
}

func TestParsePageSize_DefaultWhenAbsent(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	size, err := parsePageSize(c)

	require.NoError(t, err)
	assert.Equal(t, 100, size)
}

func TestParsePageSize_AcceptsValidRange(t *testing.T) {
	tests := []struct {
		param    string
		expected int
	}{
		{"1", 1},
		{"50", 50},
		{"100", 100},
		{"500", 500},
		{"1000", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test?pageSize="+tt.param, nil)

			size, err := parsePageSize(c)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, size)
		})
	}
}

func TestParsePageSize_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		param string
	}{
		{"zero", "0"},
		{"negative", "-1"},
		{"over_max", "1001"},
		{"non_integer", "abc"},
		{"float", "10.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/test?pageSize="+tt.param, nil)

			_, err := parsePageSize(c)

			assert.Error(t, err)
		})
	}
}

func TestEncodeDecodePageToken_RoundTrip(t *testing.T) {
	token := encodePageToken("dataset-042", "EGAF00000000099", 50, "abcdef1234567890")

	decoded, err := decodePageToken(token)

	require.NoError(t, err)
	assert.Equal(t, "dataset-042", decoded.Cursor)
	assert.Equal(t, "EGAF00000000099", decoded.CursorID)
	assert.Equal(t, 50, decoded.PageSize)
	assert.Equal(t, "abcdef1234567890", decoded.QueryHash)
	assert.True(t, decoded.Exp > time.Now().Unix())
}

func TestEncodeDecodePageToken_EmptyCursorID(t *testing.T) {
	token := encodePageToken("EGAD00000000010", "", 100, "abc123")

	decoded, err := decodePageToken(token)

	require.NoError(t, err)
	assert.Equal(t, "EGAD00000000010", decoded.Cursor)
	assert.Empty(t, decoded.CursorID)
}

func TestDecodePageToken_TamperedPayload(t *testing.T) {
	token := encodePageToken("cursor", "", 100, "hash")

	// Tamper with the payload portion (before the dot)
	parts := splitToken(t, token)
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)

	var tok pageToken
	require.NoError(t, json.Unmarshal(payload, &tok))
	tok.Cursor = "tampered"
	tampered, _ := json.Marshal(tok)

	tamperedToken := base64.RawURLEncoding.EncodeToString(tampered) + "." + parts[1]

	_, err = decodePageToken(tamperedToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

func TestDecodePageToken_ExpiredToken(t *testing.T) {
	// Create a token, then manually re-encode with expired timestamp
	tok := pageToken{
		Cursor:    "some-cursor",
		PageSize:  100,
		QueryHash: "hash",
		Exp:       time.Now().Add(-2 * time.Hour).Unix(),
	}

	payload, _ := json.Marshal(tok)
	sig := computeHMAC(payload)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := decodePageToken(token)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "page token expired")
}

func TestDecodePageToken_MalformedNoDot(t *testing.T) {
	_, err := decodePageToken("nodothere")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestDecodePageToken_MalformedBadBase64(t *testing.T) {
	_, err := decodePageToken("!!!.!!!")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestQueryFingerprint_Deterministic(t *testing.T) {
	fp1 := queryFingerprint("EGAD00000000001", "filePath", "/data/file.txt")
	fp2 := queryFingerprint("EGAD00000000001", "filePath", "/data/file.txt")

	assert.Equal(t, fp1, fp2)
	assert.Len(t, fp1, 16)
}

func TestQueryFingerprint_DifferentInputs(t *testing.T) {
	fp1 := queryFingerprint("EGAD00000000001")
	fp2 := queryFingerprint("EGAD00000000002")

	assert.NotEqual(t, fp1, fp2)
}

func TestQueryFingerprint_OrderMatters(t *testing.T) {
	fp1 := queryFingerprint("a", "b")
	fp2 := queryFingerprint("b", "a")

	assert.NotEqual(t, fp1, fp2)
}

// splitToken is a test helper that splits a page token on "." and asserts two parts.
func splitToken(t *testing.T, token string) [2]string {
	t.Helper()

	idx := -1
	for i, c := range token {
		if c == '.' {
			idx = i

			break
		}
	}

	require.NotEqual(t, -1, idx, "token must contain a dot separator")

	return [2]string{token[:idx], token[idx+1:]}
}
