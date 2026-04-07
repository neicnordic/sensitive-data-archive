package rabbitmq

import (
	"context"
	"fmt"

	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	amqp "github.com/rabbitmq/amqp091-go"
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

func (broker *rmqBroker) Subscribe(ctx context.Context, consumerGroup, sourceQueue string, handleFunc func(context.Context, broker.Message) ([]func(), error)) error {
	panic("implement me")
}
func (broker *rmqBroker) Publish(ctx context.Context, destinationQueue string, message broker.Message) error {
	panic("implement me")
}

func (broker *rmqBroker) Close() error {
	panic("implement me")
}
func (broker *rmqBroker) Alive() bool {
	panic("implement me")
}
