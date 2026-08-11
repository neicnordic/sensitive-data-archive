package mocks

import (
	"context"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2" //nolint: revive
	"github.com/stretchr/testify/mock"
)

type MockBroker struct {
	mock.Mock
}

func (m *MockBroker) Subscribe(_ context.Context, sourceQueue string, _ func(ctx context.Context, msg *broker.Message) ([]func(), error)) error {
	args := m.Called(sourceQueue)

	return args.Error(0)
}

func (m *MockBroker) Publish(_ context.Context, destinationQueue string, message broker.Message) error {
	args := m.Called(destinationQueue, message)

	return args.Error(0)
}

func (m *MockBroker) Close() error {
	args := m.Called()

	return args.Error(0)
}

func (m *MockBroker) Alive() bool {
	args := m.Called()

	return args.Bool(0)
}
