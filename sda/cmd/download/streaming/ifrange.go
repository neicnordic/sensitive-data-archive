package streaming

import (
	"net/http"
	"strings"
	"time"
)

// CheckIfRange evaluates the If-Range precondition (RFC 9110 §13.1.5).
// Returns true if Range should be honored, false if full 200 should be sent.
func CheckIfRange(ifRangeHeader string, currentETag string, lastModified time.Time) bool {
	if ifRangeHeader == "" {
		return true
	}

	// Weak ETags are not valid for If-Range (requires strong comparison)
	if strings.HasPrefix(ifRangeHeader, "W/") {
		return false
	}

	// Strong ETag comparison (starts with quote)
	if strings.HasPrefix(ifRangeHeader, "\"") {
		return ifRangeHeader == currentETag
	}

	// Otherwise treat as HTTP date
	t, err := http.ParseTime(ifRangeHeader)
	if err != nil {
		return false
	}

	if lastModified.IsZero() || !lastModified.After(t) {
		return true
	}

	return false
}
