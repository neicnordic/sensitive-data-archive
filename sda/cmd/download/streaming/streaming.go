// Package streaming provides utilities for streaming files to HTTP clients.
package streaming

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	storage "github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	log "github.com/sirupsen/logrus"
)

// RangeSpec represents a parsed HTTP Range header.
type RangeSpec struct {
	Start int64
	End   int64 // -1 means until end of file
}

// ParseRangeHeader parses an RFC 7233 Range header.
// Returns nil if no range is specified or if the range is invalid.
// Only supports a single byte range (not multiple ranges).
func ParseRangeHeader(rangeHeader string, fileSize int64) *RangeSpec {
	if rangeHeader == "" {
		return nil
	}

	// Match: bytes=START-END or bytes=START- or bytes=-SUFFIX
	re := regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)
	matches := re.FindStringSubmatch(rangeHeader)
	if matches == nil {
		log.Warnf("invalid range header format: %s", rangeHeader)

		return nil
	}

	var start, end int64

	// bytes=-SUFFIX (last N bytes)
	if matches[1] == "" && matches[2] != "" {
		suffix, err := strconv.ParseInt(matches[2], 10, 64)
		if err != nil {
			log.Warnf("invalid range suffix: %s", matches[2])

			return nil
		}
		start = fileSize - suffix
		if start < 0 {
			start = 0
		}
		end = fileSize - 1

		return &RangeSpec{Start: start, End: end}
	}

	// bytes=START- or bytes=START-END
	if matches[1] != "" {
		var err error
		start, err = strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			log.Warnf("invalid range start: %s", matches[1])

			return nil
		}
	}

	if matches[2] != "" {
		var err error
		end, err = strconv.ParseInt(matches[2], 10, 64)
		if err != nil {
			log.Warnf("invalid range end: %s", matches[2])

			return nil
		}
	} else {
		end = fileSize - 1 // Until end of file
	}

	// Validate range
	if start > end || start >= fileSize {
		log.Warnf("invalid range: start=%d, end=%d, fileSize=%d", start, end, fileSize)

		return nil
	}

	// Clamp end to file size
	if end >= fileSize {
		end = fileSize - 1
	}

	return &RangeSpec{Start: start, End: end}
}

// StreamConfig holds configuration for streaming a file.
type StreamConfig struct {
	// Writer is the HTTP response writer
	Writer http.ResponseWriter
	// NewHeader is the re-encrypted crypt4gh header
	NewHeader []byte
	// FileReader is the reader for the encrypted file (including original header)
	FileReader io.ReadSeekCloser
	// ArchiveFileSize is the total size of the encrypted file in the archive (header + body)
	ArchiveFileSize int64
	// OriginalHeaderSize is the size of the original crypt4gh header to skip
	OriginalHeaderSize int64
	// Range is the optional byte range to stream (nil for whole file)
	Range *RangeSpec
}

// StreamFile streams a file to the HTTP response writer.
// It combines the new header with the file body and handles range requests.
// The original crypt4gh header in the archive file is skipped.
func StreamFile(cfg StreamConfig) error {
	defer cfg.FileReader.Close()

	newHeaderSize := int64(len(cfg.NewHeader))
	// Body size is archive size minus the original header
	bodySize := cfg.ArchiveFileSize - cfg.OriginalHeaderSize
	totalSize := newHeaderSize + bodySize

	// Skip the original header in the archive file
	if cfg.OriginalHeaderSize > 0 {
		if _, err := cfg.FileReader.Seek(cfg.OriginalHeaderSize, io.SeekStart); err != nil {
			return fmt.Errorf("failed to skip original header: %w", err)
		}
	}

	// Create reader for the new header
	headerReader := bytes.NewReader(cfg.NewHeader)

	if cfg.Range == nil {
		// Stream whole file
		cfg.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", totalSize))
		cfg.Writer.Header().Set("Content-Type", "application/octet-stream")

		// Stream new header then body (with original header already skipped)
		if _, err := io.Copy(cfg.Writer, headerReader); err != nil {
			return fmt.Errorf("failed to stream header: %w", err)
		}
		if _, err := io.Copy(cfg.Writer, cfg.FileReader); err != nil {
			return fmt.Errorf("failed to stream body: %w", err)
		}

		return nil
	}

	// Handle range request
	rangeLength := cfg.Range.End - cfg.Range.Start + 1

	cfg.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cfg.Range.Start, cfg.Range.End, totalSize))
	cfg.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", rangeLength))
	cfg.Writer.Header().Set("Content-Type", "application/octet-stream")
	cfg.Writer.WriteHeader(http.StatusPartialContent)

	// Stream the requested range (body reader already positioned after original header)
	return streamRange(cfg.Writer, headerReader, cfg.FileReader, newHeaderSize, cfg.Range)
}

// streamRange streams a specific byte range from combined header + body.
func streamRange(w io.Writer, header *bytes.Reader, body io.ReadCloser, headerSize int64, r *RangeSpec) error {
	bytesRemaining := r.End - r.Start + 1

	// Stream from header if range overlaps
	if r.Start < headerSize {
		headerBytesStreamed, err := streamHeaderRange(w, header, r.Start, headerSize, bytesRemaining)
		if err != nil {
			return err
		}
		bytesRemaining -= headerBytesStreamed
	}

	// Stream from body if range extends into it
	if bytesRemaining > 0 {
		return streamBodyRange(w, body, r.Start, headerSize, bytesRemaining)
	}

	return nil
}

// streamHeaderRange streams bytes from the header and returns the number of bytes streamed.
func streamHeaderRange(w io.Writer, header *bytes.Reader, start, headerSize, bytesRemaining int64) (int64, error) {
	if _, err := header.Seek(start, io.SeekStart); err != nil {
		return 0, fmt.Errorf("failed to seek header: %w", err)
	}

	headerBytesToRead := headerSize - start
	if headerBytesToRead > bytesRemaining {
		headerBytesToRead = bytesRemaining
	}

	if _, err := io.CopyN(w, header, headerBytesToRead); err != nil {
		return 0, fmt.Errorf("failed to stream header range: %w", err)
	}

	return headerBytesToRead, nil
}

// streamBodyRange streams bytes from the body starting at the appropriate offset.
func streamBodyRange(w io.Writer, body io.ReadCloser, start, headerSize, bytesRemaining int64) error {
	bodyOffset := start - headerSize
	if bodyOffset < 0 {
		bodyOffset = 0
	}

	if err := seekOrSkipBody(body, bodyOffset); err != nil {
		return err
	}

	if _, err := io.CopyN(w, body, bytesRemaining); err != nil && err != io.EOF {
		return fmt.Errorf("failed to stream body range: %w", err)
	}

	return nil
}

// seekOrSkipBody positions the body reader at the given offset from the current position.
// Note: This uses io.SeekCurrent because the body reader may have already been positioned
// past the original crypt4gh header.
func seekOrSkipBody(body io.Reader, offset int64) error {
	if offset == 0 {
		return nil
	}

	if seeker, ok := body.(io.Seeker); ok {
		if _, err := seeker.Seek(offset, io.SeekCurrent); err != nil {
			return fmt.Errorf("failed to seek body: %w", err)
		}

		return nil
	}

	// Skip bytes if not seekable
	if _, err := io.CopyN(io.Discard, body, offset); err != nil {
		return fmt.Errorf("failed to skip body bytes: %w", err)
	}

	return nil
}

// SeekableMultiReader is a re-export of storage.SeekableMultiReader for convenience.
var SeekableMultiReader = storage.SeekableMultiReader
