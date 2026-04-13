package v2

import "context"

type Broker interface {
	// Subscribe starts subscribing to the specified sourceQueue through the consumerGroup and handling each incoming message with the handleFunc
	// if handleFunc returns an error the message should be nacked and marked for reconsumption
	// handleFunc supports returning a slice of callback functions that will be invocated after the message has either been acknowledged or not acknowledged
	Subscribe(ctx context.Context, consumerGroup, sourceQueue string, handleFunc func(ctx context.Context, msg *Message) ([]func(), error)) error

	// Publish publishes a message to the destinationQueue
	Publish(ctx context.Context, destinationQueue string, message Message) error

	// Close closes the broker
	Close() error

	// Alive checks whether the broker is alive(eg connections, channels, etc.)
	Alive() bool
}
