package mocks

import (
	"bytes"
	"context"
	"errors"
	"io"
)

type MockReader struct {
	Data []byte
}

func (r *MockReader) NewFileReader(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.Data)), nil
}
func (r *MockReader) NewFileReadSeeker(_ context.Context, _, _ string) (io.ReadSeekCloser, error) {
	return nil, errors.New("not implemented")
}
func (r *MockReader) FindFile(_ context.Context, _ string) (string, error) {
	return "archive", nil
}
func (r *MockReader) GetFileSize(_ context.Context, _, _ string) (int64, error) {
	return int64(len(r.Data)), nil
}
func (r *MockReader) Ping(_ context.Context) error { return nil }
