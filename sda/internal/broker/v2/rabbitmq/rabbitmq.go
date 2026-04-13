package rabbitmq

import (
	"context"
	"fmt"
	"time"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type rmqBroker struct {
	ctx context.Context

	connection         *amqp.Connection
	channel            *amqp.Channel
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

	amqpConf := amqp.Config{
		Locale: "en_US", // as per amqp.defaultLocale,
	}
	var err error

	if rmq.config.ssl {
		tlsConf, err := rmq.config.setupTlsConfig()
		if err != nil {
			return nil, err
		}
		amqpConf.TLSClientConfig = tlsConf
	}

	rmq.config.port = 5672
	rmq.config.user = "ingest"
	rmq.config.password = "ingest"
	rmq.config.vhost = "/sda"

	rmq.connection, err = amqp.DialConfig(rmq.config.buildMQURI(), amqpConf)
	if err != nil {
		return nil, err
	}

	rmq.channel, err = rmq.connection.Channel()
	if err != nil {
		return nil, err
	}

	if e := rmq.channel.Confirm(false); e != nil {
		return nil, fmt.Errorf("channel could not be put into confirm mode: %s", e)
	}

	if rmq.config.prefetchCount > 0 {
		if err := rmq.channel.Qos(rmq.config.prefetchCount, 0, false); err != nil {
			return nil, fmt.Errorf("failed to set Channel QoS to %d, reason: %v", rmq.config.prefetchCount, err)
		}
	}

	rmq.publishConfirmChan = rmq.channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return rmq, nil
}

func (b *rmqBroker) Subscribe(ctx context.Context, consumerGroup, sourceQueue string, handleFunc func(context.Context, *broker.Message) ([]func(), error)) error {
	messageChan, err := b.channel.Consume(
		sourceQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	for {
		select {
		case message, ok := <-messageChan:
			if !ok {
				log.Debugf("channel closed")
				return nil
			}
			msg := &broker.Message{
				Key:     message.CorrelationId,
				Headers: message.Headers,
				Body:    message.Body,
			}

			callbacks, err := handleFunc(ctx, msg)
			if err != nil {
				if err := message.Nack(false, false); err != nil {
					log.Errorf("failed to nack message, reason: %v", err)
				}
				continue
			}

			if err := message.Ack(false); err != nil {
				log.Errorf("failed to ack message, reason: %v", err)
			}

			for _, callback := range callbacks {
				callback()
			}

		case <-ctx.Done():
			if err := b.channel.Cancel("", true); err != nil {
				log.Errorf("failed to cancel channel, reason: %v", err)
			}
		}
	}
}

func (b *rmqBroker) Publish(ctx context.Context, destinationQueue string, message broker.Message) error {
	err := b.channel.PublishWithContext(
		ctx,
		"",
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
	return nil
}

func (b *rmqBroker) Close() error {
	if err := b.channel.Close(); err != nil {
		fmt.Errorf("failed to close broker channel connection, reason: %v", err)
	}

	if err := b.connection.Close(); err != nil {
		fmt.Errorf("failed to close broker connection, reason: %v", err)
	}

	return nil
}

func (b *rmqBroker) Alive() bool {
	return b.connection != nil && !b.connection.IsClosed() && b.channel != nil
}
