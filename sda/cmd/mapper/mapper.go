// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

var db *database.SDAdb
var inboxWriter storage.Writer
var mappings schema.DatasetMapping
var mqBroker *broker.AMQPBroker

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	var err error
	conf, err := config.NewConfig("mapper")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}

	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		return fmt.Errorf("failed to initalize sda db, due to: %v", err)
	}
	defer db.Close()
	if db.Version < 23 {
		return errors.New("database schema v23 is required")
	}

	mqBroker, err = broker.NewMQ(conf.Broker)
	if err != nil {
		return fmt.Errorf("failed to initalize mq broker, due to: %v", err)
	}
	defer func() {
		if err := mqBroker.Channel.Close(); err != nil {
			log.Errorf("failed to close mq broker channel due to: %v", err)
		}
		if err := mqBroker.Connection.Close(); err != nil {
			log.Errorf("failed to close mq broker connection due to: %v", err)
		}
	}()

	lb, err := locationbroker.NewLocationBroker(db)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker, due to: %v", err)
	}
	inboxWriter, err = storage.NewWriter(context.Background(), "inbox", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize inbox writer, due to: %v", err)
	}

	log.Info("Starting mapper service")
	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- startConsumer()
	}()

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
	case err := <-mqBroker.Connection.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-mqBroker.Channel.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-consumeErr:
		return err
	}

	return nil
}
func startConsumer() error {
	messages, err := mqBroker.GetMessages(mqBroker.Conf.Queue)
	if err != nil {
		return fmt.Errorf("failed to get message from mq (error: %v)", err)
	}

	for delivered := range messages {
		log.Debugf("received a message: %s", delivered.Body)
		schemaType, err := schemaFromDatasetOperation(delivered.Body)
		if err != nil {
			log.Errorf("%s", err.Error())
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to ack message: %v", err)
			}
			if err := mqBroker.SendMessage(delivered.CorrelationId, mqBroker.Conf.Exchange, "error", delivered.Body); err != nil {
				log.Errorf("failed to send error message: %v", err)
			}

			continue
		}

		err = schema.ValidateJSON(fmt.Sprintf("%s/%s.json", mqBroker.Conf.SchemasPath, schemaType), delivered.Body)
		if err != nil {
			log.Errorf("validation of incoming message (%s) failed, reason: %v ", schemaType, err)
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed acking canceled work, reason: %v", err)
			}

			continue
		}

		// we unmarshal the message in the validation step so this is safe to do
		_ = json.Unmarshal(delivered.Body, &mappings)

		switch mappings.Type {
		case "mapping":
			log.Debug("mapping type operation, mapping files to dataset")
			if err := db.MapFilesToDataset(mappings.DatasetID, mappings.AccessionIDs); err != nil {
				log.Errorf("failed to map files to dataset, dataset-id: %s, reason: %v", mappings.DatasetID, err)

				// Nack message so the server gets notified that something is wrong and requeue the message
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			for _, aID := range mappings.AccessionIDs {
				log.Debugf("Mapped file to dataset (correlation-id: %s, datasetid: %s, accessionid: %s)", delivered.CorrelationId, mappings.DatasetID, aID)
				fileMappingData, err := db.GetMappingData(aID)
				if err != nil {
					log.Errorf("failed to get file info for file with stable ID: %s", aID)
				}

				err = inboxWriter.RemoveFile(context.Background(), fileMappingData.SubmissionLocation, helper.UnanonymizeFilepath(fileMappingData.SubmissionFilePath, fileMappingData.User))
				if err != nil {
					log.Errorf("remove file: %s failed, reason: %v", fileMappingData.FileID, err)
				}
			}

			if err := db.UpdateDatasetEvent(mappings.DatasetID, "registered", string(delivered.Body)); err != nil {
				log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
				if err = delivered.Nack(false, false); err != nil {
					log.Errorf("failed to Nack message, reason: (%s)", err.Error())
				}

				continue
			}
		case "release":
			log.Debug("release type operation, marking dataset as released")
			if err := db.UpdateDatasetEvent(mappings.DatasetID, "released", string(delivered.Body)); err != nil {
				log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
				if err = delivered.Nack(false, false); err != nil {
					log.Errorf("failed to Nack message, reason: (%s)", err.Error())
				}

				continue
			}
		case "deprecate":
			log.Debug("deprecate type operation, marking dataset as deprecated")
			if err := db.UpdateDatasetEvent(mappings.DatasetID, "deprecated", string(delivered.Body)); err != nil {
				log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
				if err = delivered.Nack(false, false); err != nil {
					log.Errorf("failed to Nack message, reason: (%s)", err.Error())
				}

				continue
			}
		default:
			log.Errorf("unknown mapping type, %s", mappings.Type)
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: (%v)", err)
		}
	}

	return nil
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
