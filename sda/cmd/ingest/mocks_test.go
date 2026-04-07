package main

import (
	"bytes"
	"context"
	"errors"
	"io"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2" //nolint: revive
)

type MockBroker struct{}

func (m *MockBroker) Subscribe(ctx context.Context, sourceQueue string, handleFunc func(ctx context.Context, msg *broker.Message) ([]func(), error)) error {
	return nil
}

func (m *MockBroker) Publish(ctx context.Context, destinationQueue string, message broker.Message) error {
	return nil
}

func (m *MockBroker) Close() error {
	return nil
}

func (m *MockBroker) Alive() bool {
	return true
}

type MockReader struct {
	data []byte
}

func (r *MockReader) NewFileReader(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}
func (r *MockReader) NewFileReadSeeker(_ context.Context, _, _ string) (io.ReadSeekCloser, error) {
	return nil, errors.New("not implemented")
}
func (r *MockReader) FindFile(_ context.Context, _ string) (string, error) {
	return "memory", nil
}
func (r *MockReader) GetFileSize(_ context.Context, _, _ string) (int64, error) {
	return int64(len(r.data)), nil
}
func (r *MockReader) Ping(_ context.Context) error { return nil }
