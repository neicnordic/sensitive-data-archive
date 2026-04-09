package main

import (
	"context"
	"io"
	"strings"

	v2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2" //nolint: revive
)

type MockWriter struct {
	RemoveFileFunc func(ctx context.Context, location, filePath string) error
	WriteFileFunc  func(ctx context.Context, filePath string, fileContent io.Reader) (string, error)
}

func (m *MockWriter) RemoveFile(ctx context.Context, location, filePath string) error {
	if m.RemoveFileFunc != nil {
		return m.RemoveFileFunc(ctx, location, filePath)
	}

	return nil
}

func (m *MockWriter) WriteFile(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(ctx, filePath, fileContent)
	}

	return "", nil
}

type MockReader struct {
	NewReaderFunc         func(ctx context.Context, location, filePath string) (io.ReadCloser, error)
	NewFileReadSeekerFunc func(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error)
	FindFileFunc          func(ctx context.Context, filePath string) (string, error)
	GetFileSizeFunc       func(ctx context.Context, location, filePath string) (int64, error)
}

func (m *MockReader) NewFileReader(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
	if m.NewReaderFunc != nil {
		return m.NewReaderFunc(ctx, location, filePath)
	}

	return io.NopCloser(strings.NewReader("")), nil
}

func (m *MockReader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	if m.NewFileReadSeekerFunc != nil {
		return m.NewFileReadSeekerFunc(ctx, location, filePath)
	}

	return nil, nil
}

func (m *MockReader) FindFile(ctx context.Context, filePath string) (string, error) {
	if m.FindFileFunc != nil {
		return m.FindFileFunc(ctx, filePath)
	}

	return "", nil
}

func (m *MockReader) GetFileSize(ctx context.Context, location, filePath string) (int64, error) {
	if m.GetFileSizeFunc != nil {
		return m.GetFileSizeFunc(ctx, location, filePath)
	}

	return 0, nil
}

func (m *MockReader) Ping(ctx context.Context) error {
	return nil
}

type MockBroker struct{}

func (m *MockBroker) Subscribe(ctx context.Context, sourceQueue string, handleFunc func(ctx context.Context, msg *v2.Message) ([]func(), error)) error {
	return nil
}

func (m *MockBroker) Publish(ctx context.Context, destinationQueue string, message v2.Message) error {
	return nil
}

func (m *MockBroker) Close() error {
	return nil
}

func (m *MockBroker) Alive() bool {
	return true
}
