package mocks

import (
	"context"
	"io"

	"github.com/stretchr/testify/mock"
)

type MockWriter struct {
	mock.Mock
}

func (m *MockWriter) RemoveFile(_ context.Context, location, filePath string) error {
	args := m.Called(location, filePath)

	return args.Error(0)
}

func (m *MockWriter) WriteFile(_ context.Context, filePath string, _ io.Reader) (location string, err error) {
	args := m.Called(filePath)

	return args.String(0), args.Error(1)
}
