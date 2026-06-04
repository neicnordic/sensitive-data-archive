package mocks

import (
	"context"

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
