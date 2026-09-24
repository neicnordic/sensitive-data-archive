package c4ghheader

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
)

func validHeader(t *testing.T) []byte {
	t.Helper()
	pub, priv, err := keys.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := streaming.NewCrypt4GHWriter(&buf, priv, [][32]byte{pub}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	header, err := headers.ReadHeader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	return header
}

func TestValidatePacketLengthsAcceptsValidHeader(t *testing.T) {
	if err := ValidatePacketLengths(validHeader(t)); err != nil {
		t.Errorf("a real header must pass, got: %v", err)
	}
}

func TestValidatePacketLengthsRejectsShortPacket(t *testing.T) {
	// Overwrite the first packet's length field with values that underflow the
	// library's make() (4-7) or are otherwise below a valid packet.
	for _, bad := range []uint32{0, 1, 4, 7, 8, 67} {
		header := validHeader(t)
		binary.LittleEndian.PutUint32(header[headerPrefixLen:headerPrefixLen+4], bad)
		if err := ValidatePacketLengths(header); err == nil {
			t.Errorf("expected an error for packet length %d, got nil", bad)
		}
	}
}

func TestValidatePacketLengthsRejectsLengthPastBuffer(t *testing.T) {
	header := validHeader(t)
	// a large but valid uint32, well past the buffer and below the library max
	binary.LittleEndian.PutUint32(header[headerPrefixLen:headerPrefixLen+4], 1<<20)
	if err := ValidatePacketLengths(header); err == nil {
		t.Error("expected an error for a packet length past the header, got nil")
	}
}

func TestValidatePacketLengthsRejectsTruncatedPrefix(t *testing.T) {
	if err := ValidatePacketLengths([]byte("crypt4gh")); err == nil {
		t.Error("expected an error for a header shorter than the prefix, got nil")
	}
}

func TestValidatePacketLengthsRejectsMissingPacket(t *testing.T) {
	// A header that declares two packets but only carries one must be rejected,
	// so a truncated packet list cannot slip a short second packet past the walk.
	header := validHeader(t)
	binary.LittleEndian.PutUint32(header[12:16], 2) // claim two packets, only one present
	err := ValidatePacketLengths(header)
	// Assert the specific bounds-check message: a slice from bytes.Buffer has
	// spare capacity, so a missing bounds check would read zeros past the length
	// and report "too short" instead, masking the removal of the check.
	if err == nil || !strings.Contains(err.Error(), "no length field") {
		t.Errorf("expected a \"no length field\" error, got %v", err)
	}
}
