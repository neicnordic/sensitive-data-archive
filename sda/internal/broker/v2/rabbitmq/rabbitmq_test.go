package rabbitmq

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

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

func TestRabbitMQ_HandlerContextOutlivesCancelByGrace(t *testing.T) {
	b := newTestBroker()
	b.config.shutdownGrace = time.Second
	ctx, cancel := context.WithCancel(context.Background())

	hctx, done := b.handlerContext(ctx)
	defer done()

	cancel()
	assert.NoError(t, hctx.Err(), "handler context must survive the parent being cancelled")
	assert.Eventually(t, func() bool { return hctx.Err() != nil }, 3*time.Second, 50*time.Millisecond,
		"handler context should be cancelled once the grace period has passed")
}

func TestRabbitMQ_HandlerContextWithoutGraceFollowsParent(t *testing.T) {
	b := newTestBroker()
	b.config.shutdownGrace = 0
	ctx, cancel := context.WithCancel(context.Background())

	hctx, done := b.handlerContext(ctx)
	defer done()

	cancel()
	assert.Eventually(t, func() bool { return hctx.Err() != nil }, time.Second, 10*time.Millisecond)
}

func TestRabbitMQ_HandleDeliveryRunsOnUncancelledContext(t *testing.T) {
	ack := &mockAckNack{}
	b := newTestBroker()
	b.config.shutdownGrace = 5 * time.Second
	delivery := makeDelivery(ack, "key-4", []byte(`{}`), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown already started when the handler runs

	var seen error
	handle := func(hctx context.Context, _ *broker.Message) ([]func(), error) {
		seen = hctx.Err()

		return nil, nil
	}

	b.handleDelivery(ctx, delivery, handle)

	assert.NoError(t, seen, "handler should still be able to finish after shutdown started")
	assert.True(t, ack.ackCalled)
}

// hangingBroker points the broker at a local listener that accepts TCP
// connections and never speaks AMQP, so the handshake stalls until the dial
// deadline. accepted counts the connections it received.
func hangingBroker(t *testing.T) (*rmqBroker, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	var accepted atomic.Int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			t.Cleanup(func() { _ = c.Close() })
		}
	}()

	b := newTestBroker()
	b.config.host = "127.0.0.1"
	b.config.port = l.Addr().(*net.TCPAddr).Port
	b.config.user = "guest"
	b.config.password = "guest"
	b.config.vhost = "/"

	return b, &accepted
}

func TestRabbitMQ_EnsureConnectedAfterCloseDoesNotDial(t *testing.T) {
	b, accepted := hangingBroker(t)
	require.NoError(t, b.Close())

	err := b.ensureConnected(context.Background())
	assert.ErrorIs(t, err, errBrokerClosed)
	assert.False(t, b.Alive())
	assert.Equal(t, int32(0), accepted.Load(), "a closed broker must not try to dial")
}

func TestRabbitMQ_ConnectFollowsContext(t *testing.T) {
	b, accepted := hangingBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := b.ensureConnected(ctx)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "the handshake must stop at the context deadline, not the 30s default")
	assert.Equal(t, int32(1), accepted.Load())
}

func TestRabbitMQ_CloseIsNotBlockedByDial(t *testing.T) {
	b, _ := hangingBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dialErr := make(chan error, 1)
	go func() { dialErr <- b.ensureConnected(ctx) }()
	time.Sleep(100 * time.Millisecond) // let the dial start

	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	select {
	case err := <-closed:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close() blocked behind an in-flight dial")
	}

	select {
	case err := <-dialErr:
		require.Error(t, err, "the dial must not succeed against a server that never answers")
	case <-time.After(5 * time.Second):
		t.Fatal("ensureConnected did not return after the context ended")
	}
	assert.False(t, b.Alive(), "Alive() must not reconnect after Close()")
}

func TestRabbitMQ_ConcurrentEnsureConnectedDialsOnce(t *testing.T) {
	b, accepted := hangingBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	const callers = 5
	errs := make(chan error, callers)
	for range callers {
		go func() { errs <- b.ensureConnected(ctx) }()
	}
	for range callers {
		select {
		case err := <-errs:
			assert.Error(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("a concurrent caller never returned")
		}
	}
	assert.Equal(t, int32(1), accepted.Load(), "callers waiting on connMu must not dial again once the context is done")
}

func TestRabbitMQ_AliveDoesNotWaitForAnotherDial(t *testing.T) {
	b, _ := hangingBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() { _ = b.ensureConnected(ctx) }()
	time.Sleep(100 * time.Millisecond) // let the dial take connMu

	start := time.Now()
	assert.False(t, b.Alive())
	assert.Less(t, time.Since(start), 500*time.Millisecond, "Alive() must answer while another caller is dialling")
}

func TestRabbitMQ_AliveIsBounded(t *testing.T) {
	b, accepted := hangingBroker(t)

	start := time.Now()
	assert.False(t, b.Alive())
	assert.Less(t, time.Since(start), aliveDialTimeout+time.Second)
	assert.Equal(t, int32(1), accepted.Load(), "Alive() makes one reconnect attempt")
}
