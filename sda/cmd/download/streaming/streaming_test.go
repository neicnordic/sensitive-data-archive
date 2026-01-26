package streaming

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSeekCloser wraps bytes.Reader to implement io.ReadSeekCloser
type readSeekCloser struct {
	*bytes.Reader
}

func (r *readSeekCloser) Close() error {
	return nil
}

// newReadSeekCloser creates a new io.ReadSeekCloser from a byte slice
func newReadSeekCloser(data []byte) *readSeekCloser {
	return &readSeekCloser{Reader: bytes.NewReader(data)}
}

func TestParseRangeHeader_Empty(t *testing.T) {
	result, err := ParseRangeHeader("", 1000)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestParseRangeHeader_FullRange(t *testing.T) {
	result, err := ParseRangeHeader("bytes=0-499", 1000)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.Start)
	assert.Equal(t, int64(499), result.End)
}

func TestParseRangeHeader_OpenEndedRange(t *testing.T) {
	result, err := ParseRangeHeader("bytes=500-", 1000)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(500), result.Start)
	assert.Equal(t, int64(999), result.End) // End of file
}

func TestParseRangeHeader_SuffixRange(t *testing.T) {
	result, err := ParseRangeHeader("bytes=-100", 1000)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(900), result.Start)
	assert.Equal(t, int64(999), result.End)
}

func TestParseRangeHeader_SuffixLargerThanFile(t *testing.T) {
	result, err := ParseRangeHeader("bytes=-2000", 1000)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.Start) // Clamped to 0
	assert.Equal(t, int64(999), result.End)
}

func TestParseRangeHeader_EndBeyondFileSize(t *testing.T) {
	result, err := ParseRangeHeader("bytes=500-2000", 1000)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(500), result.Start)
	assert.Equal(t, int64(999), result.End) // Clamped to file size - 1
}

func TestParseRangeHeader_StartBeyondFileSize(t *testing.T) {
	result, err := ParseRangeHeader("bytes=2000-3000", 1000)
	assert.ErrorIs(t, err, ErrRangeNotSatisfiable)
	assert.Nil(t, result)
}

func TestParseRangeHeader_StartGreaterThanEnd(t *testing.T) {
	result, err := ParseRangeHeader("bytes=500-100", 1000)
	assert.ErrorIs(t, err, ErrRangeNotSatisfiable)
	assert.Nil(t, result)
}

func TestParseRangeHeader_InvalidFormat(t *testing.T) {
	// Per RFC 7233, invalid format should be ignored (no error, serve full file)
	result, err := ParseRangeHeader("invalid", 1000)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestParseRangeHeader_WrongUnit(t *testing.T) {
	// Per RFC 7233, wrong unit should be ignored (no error, serve full file)
	result, err := ParseRangeHeader("chars=0-100", 1000)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestParseRangeHeader_MultipleRanges(t *testing.T) {
	// We only support single ranges, this should be ignored (serve full file)
	result, err := ParseRangeHeader("bytes=0-100,200-300", 1000)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestParseRangeHeader_SingleByte(t *testing.T) {
	result, err := ParseRangeHeader("bytes=0-0", 1000)
	assert.NoError(t, err)
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
		FileReader:      newReadSeekCloser(body),
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
		FileReader:      newReadSeekCloser(body),
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
		FileReader:      newReadSeekCloser(body),
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
		FileReader:      newReadSeekCloser(body),
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
		FileReader:      newReadSeekCloser(body),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: totalSize - 5, End: totalSize - 1}, // Last 5 bytes
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)
	assert.Equal(t, "_DATA", recorder.Body.String())
}

// Tests for OriginalHeaderSize (skipping original crypt4gh header)

func TestStreamFile_SkipsOriginalHeader(t *testing.T) {
	// Simulates the real scenario where archive file contains [ORIGINAL_HEADER][BODY]
	// and we want to stream [NEW_HEADER][BODY]
	newHeader := []byte("NEW_CRYPT4GH_HDR")
	originalHeader := []byte("OLD_HDR_DATA") // 12 bytes to skip
	body := []byte("ENCRYPTED_FILE_BODY")
	archiveFile := make([]byte, 0, len(originalHeader)+len(body))
	archiveFile = append(archiveFile, originalHeader...)
	archiveFile = append(archiveFile, body...) // [OLD_HDR_DATA][ENCRYPTED_FILE_BODY]

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:             recorder,
		NewHeader:          newHeader,
		FileReader:         newReadSeekCloser(archiveFile),
		ArchiveFileSize:    int64(len(archiveFile)),
		OriginalHeaderSize: int64(len(originalHeader)),
		Range:              nil,
	})

	require.NoError(t, err)

	// Total size should be: newHeader + (archiveFile - originalHeader)
	// = 16 + (31 - 12) = 16 + 19 = 35
	expectedSize := len(newHeader) + len(body)
	assert.Equal(t, fmt.Sprintf("%d", expectedSize), recorder.Header().Get("Content-Length"))

	// Output should be [NEW_HEADER][BODY], not [NEW_HEADER][OLD_HEADER][BODY]
	expectedOutput := string(newHeader) + string(body)
	assert.Equal(t, expectedOutput, recorder.Body.String())

	// Verify the original header is NOT in the output
	assert.NotContains(t, recorder.Body.String(), string(originalHeader))
}

func TestStreamFile_SkipsOriginalHeader_RangeInBody(t *testing.T) {
	newHeader := []byte("NEW_HDR")           // 7 bytes
	originalHeader := []byte("ORIGINAL_HDR") // 12 bytes to skip
	body := []byte("THE_ACTUAL_BODY_DATA")   // 20 bytes
	archiveFile := make([]byte, 0, len(originalHeader)+len(body))
	archiveFile = append(archiveFile, originalHeader...)
	archiveFile = append(archiveFile, body...)

	recorder := httptest.NewRecorder()

	// Request bytes 10-19, which should be in the body portion
	// New header is 7 bytes, so bytes 10-19 means body[3:13]
	err := StreamFile(StreamConfig{
		Writer:             recorder,
		NewHeader:          newHeader,
		FileReader:         newReadSeekCloser(archiveFile),
		ArchiveFileSize:    int64(len(archiveFile)),
		OriginalHeaderSize: int64(len(originalHeader)),
		Range:              &RangeSpec{Start: 10, End: 19},
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)

	// totalSize = 7 + 20 = 27
	assert.Equal(t, "bytes 10-19/27", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "10", recorder.Header().Get("Content-Length"))
	// body[3:13] = "_ACTUAL_BO"
	assert.Equal(t, "_ACTUAL_BO", recorder.Body.String())
}

func TestStreamFile_SkipsOriginalHeader_RangeSpanningHeaderAndBody(t *testing.T) {
	newHeader := []byte("NEW_HDR")           // 7 bytes
	originalHeader := []byte("ORIGINAL_HDR") // 12 bytes to skip
	body := []byte("BODY_CONTENT")           // 12 bytes
	archiveFile := make([]byte, 0, len(originalHeader)+len(body))
	archiveFile = append(archiveFile, originalHeader...)
	archiveFile = append(archiveFile, body...)

	recorder := httptest.NewRecorder()

	// Request bytes 5-10, spanning new header (5-6) and body (7-10 -> body[0:4])
	err := StreamFile(StreamConfig{
		Writer:             recorder,
		NewHeader:          newHeader,
		FileReader:         newReadSeekCloser(archiveFile),
		ArchiveFileSize:    int64(len(archiveFile)),
		OriginalHeaderSize: int64(len(originalHeader)),
		Range:              &RangeSpec{Start: 5, End: 10},
	})

	require.NoError(t, err)
	assert.Equal(t, 206, recorder.Code)

	// totalSize = 7 + 12 = 19
	assert.Equal(t, "bytes 5-10/19", recorder.Header().Get("Content-Range"))
	assert.Equal(t, "6", recorder.Header().Get("Content-Length"))
	// newHeader[5:7] = "DR" + body[0:4] = "BODY" -> "DRBODY"
	assert.Equal(t, "DRBODY", recorder.Body.String())
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

// StreamFile validation tests

func TestStreamFile_InvalidConfig_NegativeOriginalHeaderSize(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:             recorder,
		NewHeader:          header,
		FileReader:         newReadSeekCloser(body),
		ArchiveFileSize:    int64(len(body)),
		OriginalHeaderSize: -1, // Invalid
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OriginalHeaderSize cannot be negative")
}

func TestStreamFile_InvalidConfig_NegativeArchiveFileSize(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      newReadSeekCloser(body),
		ArchiveFileSize: -1, // Invalid
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ArchiveFileSize cannot be negative")
}

func TestStreamFile_InvalidConfig_HeaderSizeExceedsArchiveSize(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:             recorder,
		NewHeader:          header,
		FileReader:         newReadSeekCloser(body),
		ArchiveFileSize:    10,
		OriginalHeaderSize: 20, // Exceeds archive size
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OriginalHeaderSize")
	assert.Contains(t, err.Error(), "cannot exceed ArchiveFileSize")
}

func TestStreamFile_InvalidRange_EndExceedsTotalSize(t *testing.T) {
	header := []byte("HEADER") // 6 bytes
	body := []byte("BODY")     // 4 bytes, totalSize = 10

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      newReadSeekCloser(body),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: 0, End: 100}, // End exceeds totalSize (10)
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "end")
	assert.Contains(t, err.Error(), "totalSize")
}

func TestStreamFile_InvalidRange_NegativeStart(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      newReadSeekCloser(body),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: -1, End: 5},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative values")
}

func TestStreamFile_InvalidRange_StartGreaterThanEnd(t *testing.T) {
	header := []byte("HEADER")
	body := []byte("BODY")

	recorder := httptest.NewRecorder()

	err := StreamFile(StreamConfig{
		Writer:          recorder,
		NewHeader:       header,
		FileReader:      newReadSeekCloser(body),
		ArchiveFileSize: int64(len(body)),
		Range:           &RangeSpec{Start: 8, End: 5},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start")
	assert.Contains(t, err.Error(), "> end")
}
