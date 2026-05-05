package rabbitmq

import (
	"context"
	"errors"
	"testing"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAckNack struct {
	ackCalled  bool
	nackCalled bool

	ackMultiple  bool
	nackMultiple bool
	nackRequeue  bool
}

func (m *mockAckNack) Ack(tag uint64, multiple bool) error {
	m.ackCalled = true
	m.ackMultiple = multiple

	return nil
}

func (m *mockAckNack) Nack(tag uint64, multiple bool, requeue bool) error {
	m.nackCalled = true
	m.nackMultiple = multiple
	m.nackRequeue = requeue

	return nil
}

func (m *mockAckNack) Reject(tag uint64, requeue bool) error { return nil }
func makeDelivery(ack *mockAckNack, correlationID string, body []byte, headers amqp.Table) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger:  ack,
		CorrelationId: correlationID,
		Body:          body,
		Headers:       headers,
	}
}

func noopHandle(_ context.Context, _ *broker.Message) ([]func(), error) {
	return nil, nil
}

func errorHandle(_ context.Context, _ *broker.Message) ([]func(), error) {
	return nil, errors.New("something went wrong")
}

func newTestBroker() *rmqBroker {
	return &rmqBroker{
		ctx:    context.Background(),
		config: defaultConfig.clone(),
	}
}

func TestRabbitMQ_AcksOnSuccess(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-1", []byte(`{}`), nil)

	b.handleDelivery(context.Background(), delivery, noopHandle)

	assert.True(t, ack.ackCalled, "Ack should be called on success")
	assert.False(t, ack.nackCalled, "Nack must not be called on success")
	assert.False(t, ack.ackMultiple, "Ack should use multiple=false")
}

func TestRabbitMQ_CallbacksRunOnSuccess(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-2", []byte(`{}`), nil)

	var ran []string
	handle := func(_ context.Context, _ *broker.Message) ([]func(), error) {
		return []func(){
			func() { ran = append(ran, "first") },
			func() { ran = append(ran, "second") },
		}, nil
	}

	b.handleDelivery(context.Background(), delivery, handle)

	require.Equal(t, []string{"first", "second"}, ran)
}

func TestRabbitMQ_NilCallbacksNosPanic(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-3", []byte(`{}`), nil)

	handle := func(_ context.Context, _ *broker.Message) ([]func(), error) {
		return nil, nil // explicitly nil slice
	}

	assert.NotPanics(t, func() {
		b.handleDelivery(context.Background(), delivery, handle)
	})
}

func TestRabbitMQ_NacksWithoutRequeueOnError(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-4", []byte(`{}`), nil)

	b.handleDelivery(context.Background(), delivery, errorHandle)

	assert.True(t, ack.nackCalled, "Nack should be called on error")
	assert.False(t, ack.nackRequeue, "Nack must use requeue=false to avoid poison-message loops")
	assert.False(t, ack.nackMultiple, "Nack should use multiple=false")
}

func TestRabbitMQ_AckNotCalledOnError(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-5", []byte(`{}`), nil)

	b.handleDelivery(context.Background(), delivery, errorHandle)

	assert.False(t, ack.ackCalled, "Ack must not be called when handleFunc returns an error")
}

func TestRabbitMQ_CallbacksRunBeforeNack(t *testing.T) {
	// Callbacks are unconditional — they must run even when handleFunc errors.
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "key-6", []byte(`{}`), nil)

	var order []string
	handle := func(_ context.Context, _ *broker.Message) ([]func(), error) {
		return []func(){
			func() { order = append(order, "cb") },
		}, errors.New("boom")
	}

	b.handleDelivery(context.Background(), delivery, handle)

	require.Equal(t, []string{"cb"}, order, "callback must run even on error")
	assert.True(t, ack.nackCalled)
}

func TestRabbitMQ_EmptyBodyAndHeaders(t *testing.T) {
	// Guard against nil-dereference when delivery carries no body or headers.
	ack := &mockAckNack{}
	b := newTestBroker()
	delivery := makeDelivery(ack, "", nil, nil)

	assert.NotPanics(t, func() {
		b.handleDelivery(context.Background(), delivery, noopHandle)
	})
	assert.True(t, ack.ackCalled)
}
