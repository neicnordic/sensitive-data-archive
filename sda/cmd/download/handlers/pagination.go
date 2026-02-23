package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type pageToken struct {
	Cursor    string `json:"c"`           // last-seen primary sort key
	CursorID  string `json:"i,omitempty"` // tie-breaker (stable_id) for files; empty for datasets
	PageSize  int    `json:"s"`           // original pageSize (reject if changed)
	QueryHash string `json:"q"`           // sha256 fingerprint of (datasetId, filters)
	Exp       int64  `json:"e"`           // Unix timestamp expiry
}

// paginationSecret holds the HMAC key. Set at startup.
var paginationSecret []byte

// SetPaginationSecret sets the HMAC signing key for page tokens.
func SetPaginationSecret(secret []byte) {
	paginationSecret = secret
}

// parsePageSize validates the "pageSize" query parameter.
// Returns a value in [1, 1000], defaulting to 100 if absent.
func parsePageSize(c *gin.Context) (int, error) {
	raw := c.Query("pageSize")
	if raw == "" {
		return 100, nil
	}

	size, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("pageSize must be an integer")
	}

	if size < 1 || size > 1000 {
		return 0, errors.New("pageSize must be between 1 and 1000")
	}

	return size, nil
}

// encodePageToken creates a signed, base64-encoded page token.
// Wire format: base64(json) + "." + base64(hmac-sha256(json)).
func encodePageToken(cursor, cursorID string, pageSize int, queryHash string) string {
	tok := pageToken{
		Cursor:    cursor,
		CursorID:  cursorID,
		PageSize:  pageSize,
		QueryHash: queryHash,
		Exp:       time.Now().Add(1 * time.Hour).Unix(),
	}

	payload, _ := json.Marshal(tok)
	sig := computeHMAC(payload)

	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// decodePageToken verifies the HMAC signature, checks expiry, and returns the parsed token.
func decodePageToken(token string) (*pageToken, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed page token")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("malformed page token")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed page token")
	}

	if !hmac.Equal(computeHMAC(payload), sig) {
		return nil, errors.New("invalid page token signature")
	}

	var tok pageToken
	if err := json.Unmarshal(payload, &tok); err != nil {
		return nil, errors.New("malformed page token")
	}

	if time.Now().Unix() > tok.Exp {
		return nil, errors.New("page token expired")
	}

	return &tok, nil
}

// queryFingerprint returns a deterministic hex-encoded prefix of SHA-256
// over the given parts joined by null bytes.
func queryFingerprint(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0x00})
		}
		h.Write([]byte(p))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// computeHMAC returns the HMAC-SHA256 of data using paginationSecret.
func computeHMAC(data []byte) []byte {
	mac := hmac.New(sha256.New, paginationSecret)
	mac.Write(data)

	return mac.Sum(nil)
}
