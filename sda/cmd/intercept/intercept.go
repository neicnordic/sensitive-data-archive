// The intercept service relays message between the queue
// provided from the federated service and local queues.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	interceptconfig "github.com/neicnordic/sensitive-data-archive/cmd/intercept/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"

	log "github.com/sirupsen/logrus"
)

type messageType string

const (
	messageTypeAccession messageType = "accession"
	messageTypeCancel    messageType = "cancel"
	messageTypeIngest    messageType = "ingest"
	messageTypeMapping   messageType = "mapping"
	messageTypeRelease   messageType = "release"
	messageTypeDeprecate messageType = "deprecate"
)

type intercept struct {
	broker  broker.Broker
	routing map[messageType]string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	app := &intercept{
		routing: map[messageType]string{
			messageTypeAccession: interceptconfig.AccessionRoutingKey(),
			messageTypeCancel:    interceptconfig.CancelRoutingKey(),
			messageTypeIngest:    interceptconfig.IngestRoutingKey(),
			messageTypeMapping:   interceptconfig.MappingRoutingKey(),
			messageTypeRelease:   interceptconfig.ReleaseRoutingKey(),
			messageTypeDeprecate: interceptconfig.DeprecateRoutingKey(),
		},
	}

	var err error
	app.broker, err = rabbitmq.NewRabbitMQBroker(ctx)
	if err != nil {
		return fmt.Errorf("failed to create new rabbit mq broker: %w", err)
	}

	defer func() {
		if app.broker == nil {
			return
		}
		if err := app.broker.Close(); err != nil {
			slog.Error("could not close broker", "error", err)
		}
	}()

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, interceptconfig.SourceQueue(), app.handleMessage)
	}()

	slog.Info("intercept service started")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", "signal", sig)

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", "error", err, "source-queue", interceptconfig.SourceQueue())

			return err
		}

		return nil
	}
}

func (app *intercept) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	msgType, err := typeFromMessage(message.Body)
	if err != nil {
		slog.Error("Failed to get type from message",
			slog.String("message-key", message.Key),
			slog.Any("error", err),
		)
		// Restart on new message
		return nil, nil
	}

	routingKey := app.routing[msgType]

	if routingKey == "" {
		slog.Warn("unknown routing key for message type, routing to undeliverable",
			slog.String("message-key", message.Key),
			slog.String("message-type", string(msgType)),
		)

		routingKey = "undeliverable"
	}

	slog.Info(
		"Routing message",
		slog.String("message-key", message.Key),
		slog.String("message-type", string(msgType)),
		slog.String("routing-key", routingKey),
	)
	if err := app.broker.Publish(ctx, routingKey, *message); err != nil {
		slog.Error("failed to publish message",
			slog.Any("error", err),
		)

		return nil, err
	}

	return nil, nil
}

// typeFromMessage returns the type value given a JSON structure for the message
// supplied in body
func typeFromMessage(body []byte) (messageType, error) {
	message := make(map[string]any)
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	msgTypeFetch, ok := message["type"]
	if !ok {
		return "", errors.New("malformed message, type is missing")
	}

	msgType, ok := msgTypeFetch.(string)
	if !ok {
		return "", errors.New("could not cast type attribute to string")
	}

	return messageType(msgType), nil
}
