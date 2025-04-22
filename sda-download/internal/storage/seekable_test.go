package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestSeekableBackend(t *testing.T) {
	for _, backendType := range []string{posixType, s3Type} {
		testConf.Type = backendType

		backend, err := NewBackend(testConf)
		assert.Nil(t, err, "Backend failed")

		var buf bytes.Buffer

		path := fmt.Sprintf("%v.%v", s3Creatable, time.Now().UnixNano())

		if testConf.Type == s3Type {
			s3back := backend.(*s3Backend)
			assert.IsType(t, s3back, &s3Backend{}, "Wrong type from NewBackend with seekable s3")
		}
		if testConf.Type == posixType {
			path, err = writeName()
			posix := backend.(*posixBackend)
			assert.Nil(t, err, "File creation for backend failed")
			assert.IsType(t, posix, &posixBackend{}, "Wrong type from NewBackend with seekable posix")
		}

		writer, err := backend.NewFileWriter(path)

		assert.NotNil(t, writer, "Got a nil reader for writer from s3")
		assert.Nil(t, err, "posix NewFileWriter failed when it shouldn't")

		for i := 0; i < 1000; i++ {
			written, err := writer.Write(writeData)
			assert.Nil(t, err, "Failure when writing to s3 writer")
			assert.Equal(t, len(writeData), written, "Did not write all writeData")
		}

		writer.Close()

		reader, err := backend.NewFileReadSeeker(path)
		assert.Nil(t, err, "s3 NewFileReadSeeker failed when it should work")
		assert.NotNil(t, reader, "Got a nil reader for s3")

		size, err := backend.GetFileSize(path)
		assert.Nil(t, err, "s3 GetFileSize failed when it should work")
		assert.Equal(t, int64(len(writeData))*1000, size, "Got an incorrect file size")

		if reader == nil {
			t.Error("reader that should be usable is not, bailing out")

			return
		}

		var readBackBuffer [4096]byte
		seeker := reader

		_, err = seeker.Read(readBackBuffer[0:4096])
		assert.Equal(t, writeData, readBackBuffer[:14], "did not read back data as expected")
		assert.Nil(t, err, "read returned unexpected error")

		if testConf.Type == s3Type {
			// POSIX is more allowing
			_, err := seeker.Seek(95000, io.SeekStart)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(-95000, io.SeekStart)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(-95000, io.SeekCurrent)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(95000, io.SeekCurrent)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(95000, io.SeekEnd)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(-95000, io.SeekEnd)
			assert.NotNil(t, err, "Seek didn't fail when it should")

			_, err = seeker.Seek(0, 4)
			assert.NotNil(t, err, "Seek didn't fail when it should")
		}

		offset, err := seeker.Seek(15, io.SeekStart)
		assert.Nil(t, err, "Seek failed when it shouldn't")
		assert.Equal(t, int64(15), offset, "Seek did not return expected offset")

		offset, err = seeker.Seek(5, io.SeekCurrent)
		assert.Nil(t, err, "Seek failed when it shouldn't")
		assert.Equal(t, int64(20), offset, "Seek did not return expected offset")

		offset, err = seeker.Seek(-5, io.SeekEnd)
		assert.Nil(t, err, "Seek failed when it shouldn't")
		assert.Equal(t, int64(13995), offset, "Seek did not return expected offset")

		n, err := seeker.Read(readBackBuffer[0:4096])
		assert.Equal(t, 5, n, "Unexpected amount of read bytes")
		assert.Nil(t, err, "Read failed when it shouldn't")

		n, err = seeker.Read(readBackBuffer[0:4096])

		assert.Equal(t, io.EOF, err, "Expected EOF")
		assert.Equal(t, 0, n, "Unexpected amount of read bytes")

		offset, err = seeker.Seek(0, io.SeekEnd)
		assert.Nil(t, err, "Seek failed when it shouldn't")
		assert.Equal(t, int64(14000), offset, "Seek did not return expected offset")

		n, err = seeker.Read(readBackBuffer[0:4096])
		assert.Equal(t, 0, n, "Unexpected amount of read bytes")
		assert.Equal(t, io.EOF, err, "Read returned unexpected error when EOF")

		offset, err = seeker.Seek(6302, io.SeekStart)
		assert.Nil(t, err, "Seek failed")
		assert.Equal(t, int64(6302), offset, "Seek did not return expected offset")

		n = 0
		for i := 0; i < 500000 && n == 0 && err == nil; i++ {
			// Allow 0 sizes while waiting for prefetch
			n, err = seeker.Read(readBackBuffer[0:4096])
		}

		assert.Equal(t, 4096, n, "Read did not return expected amounts of bytes for %v", seeker)
		assert.Equal(t, writeData[2:], readBackBuffer[:12], "did not read back data as expected")
		assert.Nil(t, err, "unexpected error when reading back data")

		offset, err = seeker.Seek(6302, io.SeekStart)
		assert.Nil(t, err, "unexpected error when seeking to read back data")
		assert.Equal(t, int64(6302), offset, "returned offset wasn't expected")

		largeBuf := make([]byte, 65536)
		readLen, err := seeker.Read(largeBuf)
		assert.Equal(t, 7698, readLen, "did not read back expected amount of data")
		assert.Nil(t, err, "unexpected error when reading back data")

		buf.Reset()

		log.SetOutput(&buf)

		if !testing.Short() {
			_, err = backend.GetFileSize(s3DoesNotExist)
			assert.NotNil(t, err, "s3 GetFileSize worked when it should not")
			assert.NotZero(t, buf.Len(), "Expected warning missing")

			buf.Reset()

			reader, err = backend.NewFileReadSeeker(s3DoesNotExist)
			assert.NotNil(t, err, "s3 NewFileReader worked when it should not")
			assert.Nil(t, reader, "Got a non-nil reader for s3")
			assert.NotZero(t, buf.Len(), "Expected warning missing")
		}

		log.SetOutput(os.Stdout)
	}
}

func TestS3SeekablePrefetchSize(t *testing.T) {
	testConf.Type = s3Type
	chunkSize := testConf.S3.Chunksize
	testConf.S3.Chunksize = 5 * 1024 * 1024
	backend, err := NewBackend(testConf)
	s3back := backend.(*s3Backend)
	assert.IsType(t, s3back, &s3Backend{}, "Wrong type from NewBackend with seekable s3")
	assert.Nil(t, err, "S3 backend failed")
	path := fmt.Sprintf("%v.%v", s3Creatable, time.Now().UnixNano())

	writer, err := backend.NewFileWriter(path)

	assert.NotNil(t, writer, "Got a nil reader for writer from s3")
	assert.Nil(t, err, "posix NewFileWriter failed when it shouldn't")

	writer.Close()

	reader, err := backend.NewFileReadSeeker(path)
	assert.Nil(t, err, "s3 NewFileReadSeeker failed when it should work")
	assert.NotNil(t, reader, "Got a nil reader for s3")

	s := reader.(*s3Reader)

	assert.Equal(t, int64(5*1024*1024), s.prefetchSize(), "Prefetch size not as expected with chunksize 5MB")
	s.Conf.Chunksize = 0
	assert.Equal(t, int64(50*1024*1024), s.prefetchSize(), "Prefetch size not as expected")

	s.Conf.Chunksize = 1024 * 1024
	assert.Equal(t, int64(50*1024*1024), s.prefetchSize(), "Prefetch size not as expected")

	testConf.S3.Chunksize = chunkSize
}

func TestS3SeekableSpecial(t *testing.T) {
	// Some special tests here, messing with internals to expose behaviour

	testConf.Type = s3Type

	backend, err := NewBackend(testConf)
	assert.Nil(t, err, "Backend failed")

	path := fmt.Sprintf("%v.%v", s3Creatable, time.Now().UnixNano())

	s3back := backend.(*s3Backend)
	assert.IsType(t, s3back, &s3Backend{}, "Wrong type from NewBackend with seekable s3")

	writer, err := backend.NewFileWriter(path)

	assert.NotNil(t, writer, "Got a nil reader for writer from s3")
	assert.Nil(t, err, "posix NewFileWriter failed when it shouldn't")

	for i := 0; i < 1000; i++ {
		written, err := writer.Write(writeData)
		assert.Nil(t, err, "Failure when writing to s3 writer")
		assert.Equal(t, len(writeData), written, "Did not write all writeData")
	}

	writer.Close()

	reader, err := backend.NewFileReadSeeker(path)
	reader.(*s3Reader).seeked = true

	assert.Nil(t, err, "s3 NewFileReader failed when it should work")
	assert.NotNil(t, reader, "Got a nil reader for s3")
	size, err := backend.GetFileSize(path)
	assert.Nil(t, err, "s3 GetFileSize failed when it should work")
	assert.Equal(t, int64(len(writeData))*1000, size, "Got an incorrect file size")

	if reader == nil {
		t.Error("reader that should be usable is not, bailing out")

		return
	}

	var readBackBuffer [4096]byte
	seeker := reader

	_, err = seeker.Read(readBackBuffer[0:4096])
	assert.Equal(t, writeData, readBackBuffer[:14], "did not read back data as expected")
	assert.Nil(t, err, "read returned unexpected error")

	err = seeker.Close()
	assert.Nil(t, err, "unexpected error when closing")

	reader, err = backend.NewFileReadSeeker(path)
	assert.Nil(t, err, "unexpected error when creating reader")

	s := reader.(*s3Reader)
	s.seeked = true
	s.prefetchAt(0)
	assert.Equal(t, 1, len(s.local), "nothing cached after prefetch")
	// Clear cache
	s.local = s.local[:0]

	s.outstandingPrefetches = []int64{0}
	t.Logf("Cache %v, outstanding %v", s.local, s.outstandingPrefetches)

	n, err := s.Read(readBackBuffer[0:4096])
	assert.Nil(t, err, "read returned unexpected error")
	assert.Equal(t, 0, n, "got data when we should get 0 because of prefetch")

	for i := 0; i < 30; i++ {
		s.local = append(s.local, s3CacheBlock{90000000, int64(0), nil})
	}
	s.prefetchAt(0)
	assert.Equal(t, 8, len(s.local), "unexpected length of cache after prefetch")

	s.outstandingPrefetches = []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s.removeFromOutstanding(9)
	assert.Equal(t, s.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s.removeFromOutstanding(19)
	assert.Equal(t, s.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s.removeFromOutstanding(5)
	// We don't care about the internal order, sort for simplicity
	slices.Sort(s.outstandingPrefetches)
	assert.Equal(t, s.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 6, 7, 8}, "unexpected outstanding prefetches after remove")

	s.objectReader = nil
	s.Bucket = ""
	s.filePath = ""
	data := make([]byte, 100)
	_, err = s.wholeReader(data)
	assert.NotNil(t, err, "wholeReader object instantiation worked when it should have failed")
}

func TestSeekableMultiReader(t *testing.T) {
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
