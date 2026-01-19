package streaming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRangeHeader_Empty(t *testing.T) {
	result := ParseRangeHeader("", 1000)
	assert.Nil(t, result)
}

func TestParseRangeHeader_FullRange(t *testing.T) {
	result := ParseRangeHeader("bytes=0-499", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.Start)
	assert.Equal(t, int64(499), result.End)
}

func TestParseRangeHeader_OpenEndedRange(t *testing.T) {
	result := ParseRangeHeader("bytes=500-", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(500), result.Start)
	assert.Equal(t, int64(999), result.End) // End of file
}

func TestParseRangeHeader_SuffixRange(t *testing.T) {
	result := ParseRangeHeader("bytes=-100", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(900), result.Start)
	assert.Equal(t, int64(999), result.End)
}

func TestParseRangeHeader_SuffixLargerThanFile(t *testing.T) {
	result := ParseRangeHeader("bytes=-2000", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.Start) // Clamped to 0
	assert.Equal(t, int64(999), result.End)
}

func TestParseRangeHeader_EndBeyondFileSize(t *testing.T) {
	result := ParseRangeHeader("bytes=500-2000", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(500), result.Start)
	assert.Equal(t, int64(999), result.End) // Clamped to file size - 1
}

func TestParseRangeHeader_StartBeyondFileSize(t *testing.T) {
	result := ParseRangeHeader("bytes=2000-3000", 1000)
	assert.Nil(t, result) // Invalid range
}

func TestParseRangeHeader_StartGreaterThanEnd(t *testing.T) {
	result := ParseRangeHeader("bytes=500-100", 1000)
	assert.Nil(t, result) // Invalid range
}

func TestParseRangeHeader_InvalidFormat(t *testing.T) {
	result := ParseRangeHeader("invalid", 1000)
	assert.Nil(t, result)
}

func TestParseRangeHeader_WrongUnit(t *testing.T) {
	result := ParseRangeHeader("chars=0-100", 1000)
	assert.Nil(t, result)
}

func TestParseRangeHeader_MultipleRanges(t *testing.T) {
	// We only support single ranges, this should fail
	result := ParseRangeHeader("bytes=0-100,200-300", 1000)
	assert.Nil(t, result)
}

func TestParseRangeHeader_SingleByte(t *testing.T) {
	result := ParseRangeHeader("bytes=0-0", 1000)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.Start)
	assert.Equal(t, int64(0), result.End)
}
