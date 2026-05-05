package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type rmqBroker struct {
	ctx context.Context
	mu  sync.Mutex

	connection         *amqp.Connection
	consumeChannel     *amqp.Channel
	publishChannel     *amqp.Channel
	publishConfirmChan <-chan amqp.Confirmation
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

	if err := rmq.connect(); err != nil {
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

func (b *rmqBroker) Alive() bool {
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
	if b.Alive() {
		return nil
	}

	const (
		initialDelay = time.Second
		maxDelay     = 30 * time.Second
	)
	delay := initialDelay

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := b.connect(); err != nil {
			log.Errorf("failed to connect, reason: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = min(delay*2, maxDelay)

			continue
		}

		log.Info("successfully reconnected to broker")

		return nil
	}
}

func (b *rmqBroker) startConsuming(sourceQueue string) (<-chan amqp.Delivery, error) {
	const (
		autoAck   = false
		exclusive = false
		noLocal   = false
		noWait    = false
	)

	b.mu.Lock()
	ch := b.consumeChannel
	b.mu.Unlock()

	return ch.Consume(sourceQueue, "", autoAck, exclusive, noLocal, noWait, nil)
}

func (b *rmqBroker) consumeMessages(ctx context.Context, messageChan <-chan amqp.Delivery, handleFunc func(context.Context, *broker.Message) ([]func(), error)) bool {
	for {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			if b.consumeChannel != nil {
				_ = b.consumeChannel.Cancel("", true)
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

	callbacks, err := handleFunc(ctx, msg)
	for _, cb := range callbacks {
		cb()
	}

	if err != nil {
		log.Errorf("error handling message %s: %v", msg.Key, err)
		delivery.Nack(false, false)

		return
	}

	delivery.Ack(false)
}

func (b *rmqBroker) connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

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

	amqpConf := amqp.Config{Locale: "en_US"}
	var err error
	if b.config.ssl {
		tlsConf, err := b.config.setupTLSConfig()
		if err != nil {
			return err
		}
		amqpConf.TLSClientConfig = tlsConf
	}

	b.connection, err = amqp.DialConfig(b.config.buildMQURI(), amqpConf)
	if err != nil {
		return fmt.Errorf("failed to dial broker: %w", err)
	}

	b.consumeChannel, err = b.connection.Channel()
	if err != nil {
		return fmt.Errorf("failed to create consume channel: %w", err)
	}

	if b.config.prefetchCount > 0 {
		if err := b.consumeChannel.Qos(b.config.prefetchCount, 0, false); err != nil {
			return fmt.Errorf("failed to set consume channel QoS to %d: %w", b.config.prefetchCount, err)
		}
	}

	b.publishChannel, err = b.connection.Channel()
	if err != nil {
		return fmt.Errorf("failed to create publish channel: %w", err)
	}

	closeChan := b.publishChannel.NotifyClose(make(chan *amqp.Error, 1))
	go func() {
		select {
		case err, ok := <-closeChan:
			if ok {
				log.Errorf("publish channel forcefully closed by server: %v", err)
			}
		case <-b.ctx.Done():
		}
	}()

	if err := b.publishChannel.Confirm(false); err != nil {
		return fmt.Errorf("publish channel could not be put into confirm mode: %w", err)
	}

	b.publishConfirmChan = b.publishChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return nil
}
