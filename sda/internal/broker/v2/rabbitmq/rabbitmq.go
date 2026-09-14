package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

var errBrokerClosed = errors.New("broker is closed, not reconnecting")

type rmqBroker struct {
	ctx context.Context
	// mu guards the connection state below. connMu serialises reconnects and
	// is never held while mu is taken, so a slow dial does not block Publish,
	// Close or Alive from reading the current state.
	mu     sync.Mutex
	connMu sync.Mutex

	connection         *amqp.Connection
	consumeChannel     *amqp.Channel
	publishChannel     *amqp.Channel
	publishConfirmChan <-chan amqp.Confirmation
	consumerTag        string
	closed             bool
	config             *options
}

func NewRabbitMQBroker(ctx context.Context, options ...func(*options)) (broker.Broker, error) {
	rmq := &rmqBroker{
		ctx:    ctx,
		config: defaultConfig.clone(),
	}

	for _, option := range options {
		option(rmq.config)
	}

	if err := rmq.connect(ctx); err != nil {
		return rmq, err
	}

	return rmq, nil
}

func (b *rmqBroker) Subscribe(ctx context.Context, sourceQueue string, handleFunc func(context.Context, *broker.Message) ([]func(), error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := b.ensureConnected(ctx); err != nil {
			return err
		}

		messageChan, err := b.startConsuming(sourceQueue)
		if err != nil {
			log.Errorf("failed to start consuming: %v", err)
			time.Sleep(time.Duration(b.config.timeout) * time.Second)

			continue
		}

		if done := b.consumeMessages(ctx, messageChan, handleFunc); done {
			return ctx.Err()
		}
	}
}

func (b *rmqBroker) Publish(ctx context.Context, destinationQueue string, message broker.Message) error {
	if err := b.ensureConnected(ctx); err != nil {
		return err
	}

	b.mu.Lock()
	ch := b.publishChannel
	confirmChan := b.publishConfirmChan
	b.mu.Unlock()

	if ch == nil {
		return errors.New("cannot publish: broker channel is not initialized")
	}

	err := ch.PublishWithContext(
		ctx,
		b.config.exchange,
		destinationQueue,
		false,
		false,
		amqp.Publishing{

			Headers:         message.Headers,
			ContentEncoding: "UTF-8",
			ContentType:     "application/json",
			DeliveryMode:    amqp.Persistent,
			CorrelationId:   message.Key,
			Priority:        0,
			Body:            message.Body,
			Timestamp:       time.Now(),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message, reason: %v", err)
	}
	select {
	case confirm, ok := <-confirmChan:
		if !ok {
			return errors.New("publish confirm channel closed")
		}
		if !confirm.Ack {
			return fmt.Errorf("publish nacked by broker for queue %s", destinationQueue)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (b *rmqBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true

	if b.publishChannel != nil {
		if err := b.publishChannel.Close(); err != nil {
			return fmt.Errorf("failed to close broker channel connection, reason: %v", err)
		}
	}

	if b.consumeChannel != nil {
		if err := b.consumeChannel.Close(); err != nil {
			return fmt.Errorf("failed to close broker channel connection, reason: %v", err)
		}
	}

	if b.connection != nil {
		if err := b.connection.Close(); err != nil {
			return fmt.Errorf("failed to close broker connection, reason: %v", err)
		}
	}

	return nil
}

// Alive reports whether the broker is connected, and reconnects first if it
// is not, so a service that only publishes can recover through its readiness
// probe. It never reconnects after Close(). While another caller is in the
// middle of a dial this waits for it, bounded by the dial and handshake
// timeouts, so a probe with a short timeout simply reports not ready.
func (b *rmqBroker) Alive() bool {
	return b.ensureConnected(b.ctx) == nil
}

func (b *rmqBroker) alive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connection == nil || b.connection.IsClosed() {
		return false
	}

	if b.publishChannel == nil || b.publishChannel.IsClosed() {
		return false
	}

	if b.consumeChannel == nil || b.consumeChannel.IsClosed() {
		return false
	}

	return true
}

func (b *rmqBroker) ensureConnected(ctx context.Context) error {
	if b.alive() {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := b.connect(ctx); err != nil {
		if errors.Is(err, errBrokerClosed) {
			return err
		}
		log.Errorf("failed to reconnect, reason: %v", err)

		return err
	}

	log.Info("successfully reconnected to broker")

	return nil
}

func (b *rmqBroker) startConsuming(sourceQueue string) (<-chan amqp.Delivery, error) {
	const (
		autoAck   = false
		exclusive = false
		noLocal   = false
		noWait    = false
	)

	// A tag we know, so the shutdown path can cancel this consumer; with ""
	// the client generates one that Cancel cannot refer to.
	tag := fmt.Sprintf("%s-%d", sourceQueue, time.Now().UnixNano())

	b.mu.Lock()
	ch := b.consumeChannel
	b.consumerTag = tag
	b.mu.Unlock()

	if ch == nil {
		return nil, errors.New("cannot consume: broker channel is not initialized")
	}

	return ch.Consume(sourceQueue, tag, autoAck, exclusive, noLocal, noWait, nil)
}

func (b *rmqBroker) consumeMessages(ctx context.Context, messageChan <-chan amqp.Delivery, handleFunc func(context.Context, *broker.Message) ([]func(), error)) bool {
	for {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			if b.consumeChannel != nil {
				if err := b.consumeChannel.Cancel(b.consumerTag, false); err != nil {
					log.Debugf("cancelling consumer during shutdown: %v", err)
				}
			}
			b.mu.Unlock()

			return true

		case delivery, ok := <-messageChan:
			if !ok {
				log.Warn("RabbitMQ consumption channel closed, preparing to recover...")

				return false
			}
			b.handleDelivery(ctx, delivery, handleFunc)
		}
	}
}

func (b *rmqBroker) handleDelivery(ctx context.Context, delivery amqp.Delivery, handleFunc func(context.Context, *broker.Message) ([]func(), error)) {
	msg := &broker.Message{
		Key:     delivery.CorrelationId,
		Headers: delivery.Headers,
		Body:    delivery.Body,
	}

	// The handler keeps running after shutdown starts, so the message it is
	// working on can be finished and acked instead of being cut off.
	hctx, done := b.handlerContext(ctx)
	defer done()

	callbacks, err := handleFunc(hctx, msg)
	if err != nil {
		delivery.Nack(false, true)
	} else {
		delivery.Ack(false)
	}

	for _, cb := range callbacks {
		cb()
	}
}

// handlerContext returns a context for one handler call. It carries the
// values of ctx but is not cancelled with it: when ctx is cancelled the
// handler gets shutdownGrace to finish before hctx is cancelled.
// A grace of 0 keeps the old behaviour, where the handler stops with ctx.
func (b *rmqBroker) handlerContext(ctx context.Context) (context.Context, context.CancelFunc) {
	hctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	grace := b.config.shutdownGrace

	stop := context.AfterFunc(ctx, func() {
		if grace <= 0 {
			cancel()

			return
		}
		time.AfterFunc(grace, cancel)
	})

	return hctx, func() {
		stop()
		cancel()
	}
}

// connect (re)establishes the connection and both channels. Reconnects are
// serialised on connMu and the state is re-checked under it, so two callers
// that both saw a dead connection result in one dial, and a caller that
// arrives after Close() does not reconnect. The dial itself runs without mu
// held; the new state is swapped in under mu only once everything succeeded.
func (b *rmqBroker) connect(ctx context.Context) error {
	b.connMu.Lock()
	defer b.connMu.Unlock()

	if b.alive() {
		return nil
	}

	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errBrokerClosed
	}

	conn, err := b.dial(ctx)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Close() may have run while we were dialling.
	if b.closed {
		conn.connection.Close()

		return errBrokerClosed
	}

	b.closeLocked()
	b.connection = conn.connection
	b.consumeChannel = conn.consumeChannel
	b.publishChannel = conn.publishChannel
	b.publishConfirmChan = conn.publishConfirmChan

	return nil
}

type brokerConn struct {
	connection         *amqp.Connection
	consumeChannel     *amqp.Channel
	publishChannel     *amqp.Channel
	publishConfirmChan <-chan amqp.Confirmation
}

// dial opens a new connection with both channels set up. The TCP dial follows
// ctx; the TLS and AMQP handshakes get the same deadline amqp091-go's default
// dialer uses, which the library clears once the connection is open.
func (b *rmqBroker) dial(ctx context.Context) (*brokerConn, error) {
	const handshakeTimeout = 30 * time.Second

	amqpConf := amqp.Config{
		Locale: "en_US",
		Dial: func(network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: handshakeTimeout}
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
				conn.Close()

				return nil, err
			}

			return conn, nil
		},
	}
	if b.config.ssl {
		tlsConf, err := b.config.setupTLSConfig()
		if err != nil {
			return nil, err
		}
		amqpConf.TLSClientConfig = tlsConf
	}

	connection, err := amqp.DialConfig(b.config.buildMQURI(), amqpConf)
	if err != nil {
		return nil, fmt.Errorf("failed to dial broker: %w", err)
	}

	c, err := b.openChannels(connection)
	if err != nil {
		connection.Close()

		return nil, err
	}

	return c, nil
}

func (b *rmqBroker) openChannels(connection *amqp.Connection) (*brokerConn, error) {
	consumeChannel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to create consume channel: %w", err)
	}

	if b.config.prefetchCount > 0 {
		if err := consumeChannel.Qos(b.config.prefetchCount, 0, false); err != nil {
			return nil, fmt.Errorf("failed to set consume channel QoS to %d: %w", b.config.prefetchCount, err)
		}
	}

	publishChannel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to create publish channel: %w", err)
	}

	closeChan := publishChannel.NotifyClose(make(chan *amqp.Error, 1))
	go func() {
		select {
		case err, ok := <-closeChan:
			if ok {
				log.Errorf("publish channel forcefully closed by server: %v", err)
			}
		case <-b.ctx.Done():
		}
	}()

	if err := publishChannel.Confirm(false); err != nil {
		return nil, fmt.Errorf("publish channel could not be put into confirm mode: %w", err)
	}

	return &brokerConn{
		connection:         connection,
		consumeChannel:     consumeChannel,
		publishChannel:     publishChannel,
		publishConfirmChan: publishChannel.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}, nil
}

// closeLocked drops the current connection state before a reconnect. Errors
// are expected here, the connection is usually already gone. Caller holds mu.
func (b *rmqBroker) closeLocked() {
	if b.consumeChannel != nil {
		if err := b.consumeChannel.Close(); err != nil {
			log.Debugf("closing consume channel during reconnect: %v", err)
		}
		b.consumeChannel = nil
	}

	if b.publishChannel != nil {
		if err := b.publishChannel.Close(); err != nil {
			log.Debugf("closing publish channel during reconnect: %v", err)
		}
		b.publishChannel = nil
	}

	if b.connection != nil {
		if err := b.connection.Close(); err != nil {
			log.Debugf("closing connection during reconnect: %v", err)
		}
		b.connection = nil
	}
}
