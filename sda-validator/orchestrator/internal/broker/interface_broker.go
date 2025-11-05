package broker

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

type AMQPBrokerI interface {
	// PublishMessage to the exchange configured on the broker creation with the destination as the routing key
	PublishMessage(ctx context.Context, destination string, body []byte) error
	// Subscribe creates a consumer on the queue, where each message will be handled with the handleFunc
	Subscribe(ctx context.Context, queue, consumerID string, handleFunc func(context.Context, amqp.Delivery) error) error
	// Close the broker connection
	Close() error
	// Monitor returns a chan watching the broker connection and channel close events
	Monitor() chan *amqp.Error
}
