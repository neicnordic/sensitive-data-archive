package streaming

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// StreamFile tests

func TestStreamFile_WholeFile(t *testing.T) {
	header := []byte("CRYPT4GH_HEADER_DATA")
	body := []byte("FILE_BODY_CONTENT_HERE")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      io.NopCloser(bytes.NewReader(body)),
		ArchiveFileSize: int64(len(body)),
		Range:           nil,
	})

	require.NoError(t, err)
	assert.Equal(t, "42", recorder.Header().Get("Content-Length"))
	assert.Equal(t, "application/octet-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, string(header)+string(body), recorder.Body.String())
}

func TestStreamFile_RangeRequest_HeaderOnly(t *testing.T) {
	header := []byte("CRYPT4GH_HEADER_DATA") // 20 bytes
	body := []byte("FILE_BODY_CONTENT")      // 17 bytes

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      io.NopCloser(bytes.NewReader(body)),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: 0, End: 9}, // First 10 bytes (header only)
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)
	assert.Equal(t, "bytes 0-9/37", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "10", recorder.Header().Get("Content-Length"))
	assert.Equal(t, "CRYPT4GH_H", recorder.Body.String())
}

func TestStreamFile_RangeRequest_BodyOnly(t *testing.T) {
	header := []byte("CRYPT4GH_HEADER_DATA") // 20 bytes
	body := []byte("FILE_BODY_CONTENT")      // 17 bytes

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      io.NopCloser(bytes.NewReader(body)),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: 25, End: 30}, // Bytes 25-30 (body only)
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)
	assert.Equal(t, "bytes 25-30/37", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "6", recorder.Header().Get("Content-Length"))
	// bodyOffset = 25 - 20 = 5, so we read body[5:11] = "BODY_C" (wait, body is "FILE_BODY_CONTENT")
	// body[5:11] = "BODY_C" - correct!
	assert.Equal(t, "BODY_C", recorder.Body.String())
}

func TestStreamFile_RangeRequest_SpanningHeaderAndBody(t *testing.T) {
	header := []byte("CRYPT4GH_HEADER_DATA") // 20 bytes
	body := []byte("FILE_BODY_CONTENT")      // 17 bytes

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      io.NopCloser(bytes.NewReader(body)),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: 15, End: 24}, // Last 5 bytes of header + first 5 bytes of body
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)
	assert.Equal(t, "bytes 15-24/37", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "10", recorder.Header().Get("Content-Length"))
	assert.Equal(t, "_DATAFILE_", recorder.Body.String()) // header[15:20] + body[0:5]
}

func TestStreamFile_RangeRequest_LastBytes(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY_DATA")

	recorder := httptest.NewRecorder()
	totalSize := int64(len(header) + len(body)) // 15

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      io.NopCloser(bytes.NewReader(body)),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: totalSize - 5, End: totalSize - 1}, // Last 5 bytes
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)
	assert.Equal(t, "_DATA", recorder.Body.String())
}

// seekOrSkipBody tests

type nonSeekableReader struct {
	*bytes.Reader
}

func (r *nonSeekableReader) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

func TestSeekOrSkipBody_ZeroOffset(t *testing.T) {
	body := bytes.NewReader([]byte("test data"))
	err := seekOrSkipBody(body, 0)
	require.NoError(t, err)

	// Should still be at the beginning
	data, _ := io.ReadAll(body)
	assert.Equal(t, "test data", string(data))
}

func TestSeekOrSkipBody_Seekable(t *testing.T) {
	body := bytes.NewReader([]byte("test data"))
	err := seekOrSkipBody(body, 5)
	require.NoError(t, err)

	// Should be at position 5
	data, _ := io.ReadAll(body)
	assert.Equal(t, "data", string(data))
}

func TestSeekOrSkipBody_NonSeekable(t *testing.T) {
	body := &nonSeekableReader{bytes.NewReader([]byte("test data"))}
	err := seekOrSkipBody(body, 5)
	require.NoError(t, err)

	// Should have skipped 5 bytes
	data, _ := io.ReadAll(body)
	assert.Equal(t, "data", string(data))
}
