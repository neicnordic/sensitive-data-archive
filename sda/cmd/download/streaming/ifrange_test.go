package streaming

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckIfRange_EmptyHeader(t *testing.T) {
	assert.True(t, CheckIfRange("", `"abc"`, time.Now()))
}

func TestCheckIfRange_MatchingStrongETag(t *testing.T) {
	assert.True(t, CheckIfRange(`"etag123"`, `"etag123"`, time.Time{}))
}

func TestCheckIfRange_MismatchingStrongETag(t *testing.T) {
	assert.False(t, CheckIfRange(`"etag123"`, `"etag456"`, time.Time{}))
}

func TestCheckIfRange_WeakETag(t *testing.T) {
	assert.False(t, CheckIfRange(`W/"etag123"`, `"etag123"`, time.Time{}))
}

func TestCheckIfRange_ValidDate_NotModified(t *testing.T) {
	lastModified := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ifRange := "Wed, 01 Jan 2025 00:00:00 GMT"
	assert.True(t, CheckIfRange(ifRange, "", lastModified))
}

func TestCheckIfRange_ValidDate_ModifiedAfter(t *testing.T) {
	lastModified := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ifRange := "Wed, 01 Jan 2025 00:00:00 GMT"
	assert.False(t, CheckIfRange(ifRange, "", lastModified))
}

func TestCheckIfRange_InvalidDate(t *testing.T) {
	assert.False(t, CheckIfRange("not-a-date", "", time.Now()))
}
