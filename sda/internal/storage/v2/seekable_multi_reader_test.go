package storage

import (
	"bytes"
	"io"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestSeekableMultiReader(t *testing.T) {
	writeData := []byte("this is a test")

	readers := make([]io.Reader, 10)
	for i := 0; i < 10; i++ {
		readers[i] = bytes.NewReader(writeData)
	}

	seeker, err := SeekableMultiReader(readers...)
	assert.Nil(t, err, "unexpected error from creating SeekableMultiReader")

	var readBackBuffer [4096]byte

	_, err = seeker.Read(readBackBuffer[0:4096])
	assert.Equal(t, writeData, readBackBuffer[:14], "did not read back data as expected")
	assert.Nil(t, err, "unexpected error from read")

	offset, err := seeker.Seek(60, io.SeekStart)

	assert.Nil(t, err, "Seek failed")
	assert.Equal(t, int64(60), offset, "Seek did not return expected offset")

	// We don't know how many bytes this should return
	_, err = seeker.Read(readBackBuffer[0:4096])
	assert.Equal(t, writeData[4:], readBackBuffer[:10], "did not read back data as expected")
	assert.Nil(t, err, "Read failed when it should not")

	offset, err = seeker.Seek(0, io.SeekEnd)
	assert.Equal(t, int64(140), offset, "Seek did not return expected offset")
	assert.Nil(t, err, "Seek failed when it should not")

	n, err := seeker.Read(readBackBuffer[0:4096])

	assert.Equal(t, 0, n, "Read did not return expected amounts of bytes")
	assert.Equal(t, io.EOF, err, "did not get EOF as expected")

	offset, err = seeker.Seek(56, io.SeekStart)
	assert.Equal(t, int64(56), offset, "Seek did not return expected offset")
	assert.Nil(t, err, "Seek failed unexpectedly")

	largeBuf := make([]byte, 65536)
	readLen, err := seeker.Read(largeBuf)
	assert.Nil(t, err, "unexpected error when reading back data")
	assert.Equal(t, 14, readLen, "did not read back expect amount of data")

	log.SetOutput(os.Stdout)
}
