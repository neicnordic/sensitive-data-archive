package mocks

import (
	"bytes"
	"context"
	"io"

	"github.com/stretchr/testify/mock"
)

type MockReader struct {
	mock.Mock
}

type readSeekCloser struct {
	*bytes.Reader
}

func (readSeekCloser) Close() error { return nil }

func (r *MockReader) NewFileReader(_ context.Context, location, filePath string) (io.ReadCloser, error) {
	args := r.Called(location, filePath)
	content, err := args.Get(0), args.Error(1)
	if content != nil {
		return readSeekCloser{bytes.NewReader(content.([]byte))}, err
	}

	return nil, err
}
func (r *MockReader) NewFileReadSeeker(_ context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	args := r.Called(location, filePath)
	content, err := args.Get(0), args.Error(1)
	if content != nil {
		return readSeekCloser{bytes.NewReader(content.([]byte))}, err
	}

	return nil, err
}
func (r *MockReader) FindFile(_ context.Context, filePath string) (string, error) {
	args := r.Called(filePath)

	return args.String(0), args.Error(1)
}
func (r *MockReader) GetFileSize(_ context.Context, location, filePath string) (int64, error) {
	args := r.Called(location, filePath)

	return args.Get(0).(int64), args.Error(1)
}
func (r *MockReader) Ping(_ context.Context) error {
	args := r.Called()

	return args.Error(0)
}
