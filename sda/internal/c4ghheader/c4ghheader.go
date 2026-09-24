// Package c4ghheader validates a Crypt4GH header before it is handed to the
// crypt4gh library. The library (v1.15.0) trusts the per-packet length fields:
// a length below the minimum underflows an unsigned subtraction and makes it
// allocate gigabytes, a fatal out-of-memory that a recover cannot catch. This
// package rejects such a header up front so a crafted upload cannot exhaust a
// service's memory. It can be dropped once the library validates lengths itself.
package c4ghheader

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// magicLen + version(4) + packet count(4).
	headerPrefixLen = 8 + 4 + 4

	// minPacketLen is the smallest a header packet can be: the 4-byte length,
	// the 4-byte encryption method and an encrypted payload holding at least a
	// writer public key, a nonce and the Poly1305 tag.
	minPacketLen = 4 + 4 + chacha20poly1305.KeySize + chacha20poly1305.NonceSize + chacha20poly1305.Overhead
)

// ValidatePacketLengths walks the packet length fields of a Crypt4GH header and
// returns an error if any packet is shorter than a valid packet or runs past
// the buffer. It does not decrypt anything; it only guards the length fields the
// crypt4gh library trusts. A header that passes can still be rejected by the
// library for other reasons.
func ValidatePacketLengths(header []byte) error {
	if len(header) < headerPrefixLen {
		return fmt.Errorf("crypt4gh header is too short (%d bytes)", len(header))
	}

	packetCount := binary.LittleEndian.Uint32(header[12:16])
	offset := headerPrefixLen
	for i := uint32(0); i < packetCount; i++ {
		if offset+4 > len(header) {
			return fmt.Errorf("crypt4gh header packet %d has no length field", i)
		}
		packetLen := binary.LittleEndian.Uint32(header[offset : offset+4])
		if packetLen < minPacketLen {
			return fmt.Errorf("crypt4gh header packet %d length %d is too short to be valid", i, packetLen)
		}
		if int64(offset)+int64(packetLen) > int64(len(header)) {
			return fmt.Errorf("crypt4gh header packet %d length %d runs past the header", i, packetLen)
		}
		offset += int(packetLen)
	}

	return nil
}
