// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	log "github.com/sirupsen/logrus"
)

var (
	inbox    storage.Backend
	mq       *broker.AMQPBroker
	conf     *config.Config
	db       *database.SDAdb
	mappings schema.DatasetMapping
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := observability.SetupOTelSDK(ctx, "mapper")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		if err := otelShutdown(ctx); err != nil {
			log.Errorf("failed to shutdown otel: %v", err)
		}
	}()

	ctx, span := observability.GetTracer().Start(ctx, "startUp")

	forever := make(chan bool)
	conf, err = config.NewConfig("mapper")
	if err != nil {
		log.Fatal(err)
	}
	mq, err = broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	inbox, err = storage.NewBackend(ctx, conf.Inbox)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()
	defer db.Close()

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := mq.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("Starting mapper service")
	span.End()

	go func() {
		messages, err := mq.GetMessages(ctx, conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get message from mq (error: %v)", err)
		}

		for msg := range messages {
			ctx, span := observability.GetTracer().Start(msg.Context(), "handleMessage", trace.WithAttributes(attribute.String("correlation-id", msg.Message.CorrelationId)))

			if err := handleMessage(ctx, msg.Message); err != nil {
				// TODO err handle
				span.End()
				log.Fatal(err)
			}

			span.End()
		}
	}()

	<-forever
}

// schemaFromDatasetOperation returns the operation done with dataset supplied in body of the message
func schemaFromDatasetOperation(body []byte) (string, error) {
	message := make(map[string]any)
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	datasetMessageType, ok := message["type"]
	if !ok {
		return "", errors.New("malformed message, dataset message type is missing")
	}

	datasetOpsType, ok := datasetMessageType.(string)
	if !ok {
		return "", errors.New("could not cast operation attribute to string")
	}

	switch datasetOpsType {
	case "mapping":
		return "dataset-mapping", nil
	case "release":
		return "dataset-release", nil
	case "deprecate":
		return "dataset-deprecate", nil
	default:
		return "", errors.New("could not recognize mapping operation")
	}
}

func handleMessage(ctx context.Context, delivered amqp.Delivery) error {

	log.Debugf("received a message: %s", delivered.Body)
	schemaType, err := schemaFromDatasetOperation(delivered.Body)
	if err != nil {
		log.Errorf("%s", err.Error())
		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to ack message: %v", err)
		}
		if err := mq.SendMessage(ctx, delivered.CorrelationId, mq.Conf.Exchange, "error", delivered.Body); err != nil {
			log.Errorf("failed to send error message: %v", err)
		}

		return nil
	}

	err = schema.ValidateJSON(fmt.Sprintf("%s/%s.json", conf.Broker.SchemasPath, schemaType), delivered.Body)
	if err != nil {
		log.Errorf("validation of incoming message (%s) failed, reason: %v ", schemaType, err)
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: %v", err)
		}

		return nil
	}

	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(delivered.Body, &mappings)

	switch mappings.Type {
	case "mapping":
		log.Debug("Mapping type operation, mapping files to dataset")
		if err := db.MapFilesToDataset(ctx, mappings.DatasetID, mappings.AccessionIDs); err != nil {
			log.Errorf("failed to map files to dataset, dataset-id: %s, reason: %v", mappings.DatasetID, err)

			// Nack message so the server gets notified that something is wrong and requeue the message
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Nack message, reason: (%v)", err)
			}

			return nil
		}

		for _, aID := range mappings.AccessionIDs {
			log.Debugf("Mapped file to dataset (corr-id: %s, datasetid: %s, accessionid: %s)", delivered.CorrelationId, mappings.DatasetID, aID)
			filePath, err := db.GetInboxPath(ctx, aID)
			if err != nil {
				log.Errorf("failed to get inbox path for file with stable ID: %s", aID)
			}
			err = inbox.RemoveFile(ctx, filePath)
			if err != nil {
				log.Errorf("Remove file from inbox failed, reason: %v", err)
			}
		}

		if err := db.UpdateDatasetEvent(ctx, mappings.DatasetID, "registered", string(delivered.Body)); err != nil {
			log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
			if err = delivered.Nack(false, false); err != nil {
				log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}
	case "release":
		log.Debug("Release type operation, marking dataset as released")
		if err := db.UpdateDatasetEvent(ctx, mappings.DatasetID, "released", string(delivered.Body)); err != nil {
			log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
			if err = delivered.Nack(false, false); err != nil {
				log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}
	case "deprecate":
		log.Debug("Deprecate type operation, marking dataset as deprecated")
		if err := db.UpdateDatasetEvent(ctx, mappings.DatasetID, "deprecated", string(delivered.Body)); err != nil {
			log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
			if err = delivered.Nack(false, false); err != nil {
				log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}
	default:
		log.Errorf("unknown mapping type, %s", mappings.Type)
	}

	if err := delivered.Ack(false); err != nil {
		log.Errorf("failed to Ack message, reason: (%v)", err)
	}
	return nil
}
