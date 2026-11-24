package v2

import "context"

type Broker interface {
	// Subscribe starts subscribing to the specified sourceQueue through the consumerGroup and handling each incoming message with the handleFunc
	// if handleFunc returns an error the message should be nacked and marked for reconsumption
	// note that this holds for classic and quorum queues only, RabbitMQ streams ignore the settle reason,
	// so a returned error is not redelivered and a stream consumer needs its own retry or error-queue path
	// handleFunc can return a slice of callbacks that trigger after acknowledgment of the message's regardless of acknowledgment status.
	Subscribe(ctx context.Context, sourceQueue string, handleFunc func(ctx context.Context, msg *Message) ([]func(), error)) error

	// Publish publishes a message to the destinationQueue
	Publish(ctx context.Context, destinationQueue string, message Message) error

	// Close closes the broker
	Close() error

	// Alive checks whether the broker is alive(eg connections, channels, etc.)
	Alive() bool
}
