package mocks

import (
	"context"
	"io"
)

type MockWriter struct{}

func (m *MockWriter) RemoveFile(_ context.Context, location, filePath string) error {
	return nil
}

func (m *MockWriter) WriteFile(_ context.Context, filePath string, _ io.Reader) (location string, err error) {
	return "archive", nil
}
