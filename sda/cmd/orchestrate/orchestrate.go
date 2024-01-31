// The orchestrate service plays the role of processing messages
// in stand-alone operations.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"

	uuid "github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type upload struct {
	Operation          string      `json:"operation"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	Filesize           int         `json:"filesize"`
	LastModified       string      `json:"file_last_modified,omitempty"`
	EncryptedChecksums []checksums `json:"encrypted_checksums,omitempty"`
}

type request struct {
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	DecryptedChecksums []checksums `json:"decrypted_checksums"`
}

type trigger struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	EncryptedChecksums []checksums `json:"encrypted_checksums"`
}

type finalize struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []checksums `json:"decrypted_checksums"`
}

type mapping struct {
	Type         string   `json:"type"`
	DatasetID    string   `json:"dataset_id"`
	AccessionIDs []string `json:"accession_ids"`
}

type checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func main() {
	conf, err := config.NewConfig("orchestrate")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()

	queues := []string{conf.Orchestrator.QueueInbox, conf.Orchestrator.QueueVerify, conf.Orchestrator.QueueComplete}

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		os.Exit(1)
	}()

	routing := map[string]string{
		conf.Orchestrator.QueueVerify:   conf.Orchestrator.QueueAccession,
		conf.Orchestrator.QueueInbox:    conf.Orchestrator.QueueIngest,
		conf.Orchestrator.QueueComplete: conf.Orchestrator.QueueMapping,
	}

	forever := make(chan bool)

	log.Info("Starting orchestrate service")

	for _, queue := range queues {
		routingKey := routing[queue]
		go processQueue(mq, queue, routingKey, conf)
	}
	<-forever
}

func processQueue(mq *broker.AMQPBroker, queue string, routingKey string, conf *config.Config) {
	durable := conf.Broker.Durable

	log.Infof("Monitoring queue: %s", queue)

	messages, err := mq.GetMessages(queue)
	if err != nil {
		log.Fatal(err)
	}
	for delivered := range messages {
		log.Debugf("Received a message: %s", delivered.Body)

		schema, err := schemaNameFromQueue(queue, delivered.Body, conf)

		if err != nil {
			log.Errorf(err.Error())

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to ack message: %v", err)
			}
			if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, "error", durable, delivered.Body); err != nil {
				log.Errorf("failed to send error message: %v", err)
			}

			continue
		}

		err = mq.ValidateJSON(&delivered, schema, delivered.Body, nil)

		if err != nil {
			log.Errorf("Message validation failed (schema: %v, error: %v, message: %s)", schema, err, delivered.Body)

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to ack message: %v", err)
			}
			if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, "error", durable, delivered.Body); err != nil {
				log.Errorf("failed to send error message: %v", err)
			}

			continue
		}

		var publishMsg []byte
		var publishType interface{}

		routingSchema, err := schemaNameFromQueue(routingKey, nil, conf)

		if err != nil {
			log.Errorf("Don't know schema for routing key: %v", routingKey)

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to ack message: %v", err)
			}
			if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, "error", durable, delivered.Body); err != nil {
				log.Errorf("failed to send error message: %v", err)
			}

			continue
		}

		switch routingKey {
		case conf.Orchestrator.QueueAccession:
			publishMsg, publishType = finalizeMessage(delivered.Body, conf)
			err = validateMsg(&delivered, mq, routingKey, durable, routingSchema, publishMsg, publishType)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, error: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to nack message for reason: %v", err)
				}

				continue
			}
		case conf.Orchestrator.QueueIngest:
			publishMsg, publishType = ingestMessage(delivered.Body)
			err = validateMsg(&delivered, mq, routingKey, durable, routingSchema, publishMsg, publishType)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, error: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to nack message for reason: %v", err)
				}

				continue
			}
		case conf.Orchestrator.QueueMapping:
			publishMsg, publishType = mappingMessage(delivered.Body, conf)
			err = validateMsg(&delivered, mq, routingKey, durable, routingSchema, publishMsg, publishType)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, error: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to nack message for reason: %v", err)
				}

				continue
			}
			// let us wait a minute before sending the release message
			time.Sleep(conf.Orchestrator.ReleaseDelay * time.Minute)
			publishMsg, publishType = releaseMessage(delivered.Body, conf)
			err = validateMsg(&delivered, mq, routingKey, durable, routingSchema, publishMsg, publishType)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, error: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to nack message for reason: %v", err)
				}

				continue
			}
		}

	}
}

// schemaNameFromQueue returns the schema to use for messages
// determined by the queue
func schemaNameFromQueue(queue string, body []byte, conf *config.Config) (string, error) {
	if queue == conf.Orchestrator.QueueInbox {
		return schemaFromInboxOperation(body)
	}
	if queue == conf.Orchestrator.QueueMapping {
		return schemaFromDatasetOperation(body)
	}
	m := map[string]string{
		conf.Orchestrator.QueueVerify:    "ingestion-accession-request",
		conf.Orchestrator.QueueComplete:  "ingestion-completion",
		conf.Orchestrator.QueueIngest:    "ingestion-trigger",
		conf.Orchestrator.QueueAccession: "ingestion-accession",
		conf.Orchestrator.QueueBackup:    "ingestion-completion",
	}

	if m[queue] != "" {
		return m[queue], nil
	}

	return "", fmt.Errorf("Don't know what schema to use for %s", queue)
}

// schemaFromInboxOperation returns the operation done in inbox
// supplied in body of the message
func schemaFromInboxOperation(body []byte) (string, error) {
	message := make(map[string]interface{})
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	inboxOperationFetch, ok := message["operation"]
	if !ok {
		return "", errors.New("Malformed message, inbox operation is missing")
	}

	inboxOps, ok := inboxOperationFetch.(string)
	if !ok {
		return "", errors.New("Could not cast operation attribute to string")
	}

	switch inboxOps {
	case "upload":
		return "inbox-upload", nil
	case "rename":
		return "inbox-rename", nil
	case "remove":
		return "inbox-remove", nil
	default:
		return "", errors.New("Could not recognize inbox operation")
	}

}

// schemaFromDatasetOperation returns the operation done with dataset
// supplied in body of the message
func schemaFromDatasetOperation(body []byte) (string, error) {
	message := make(map[string]interface{})
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	datasetMessageType, ok := message["type"]
	if !ok {
		return "", errors.New("Malformed message, dataset message type is missing")
	}

	datasetOpsType, ok := datasetMessageType.(string)
	if !ok {
		return "", errors.New("Could not cast operation attribute to string")
	}

	switch datasetOpsType {
	case "mapping":
		return "dataset-mapping", nil
	case "release":
		return "dataset-release", nil
	case "deprecate":
		return "dataset-deprecate", nil
	default:
		return "", errors.New("Could not recognize inbox operation")
	}

}

func ingestMessage(body []byte) ([]byte, interface{}) {
	var message upload
	err := json.Unmarshal(body, &message)
	if err != nil {
		return nil, nil
	}

	msg := trigger{
		Type:               "ingest",
		User:               message.User,
		Filepath:           message.Filepath,
		EncryptedChecksums: message.EncryptedChecksums,
	}

	publish, _ := json.Marshal(&msg)

	return publish, new(trigger)
}

func finalizeMessage(body []byte, conf *config.Config) ([]byte, interface{}) {
	var message request
	err := json.Unmarshal(body, &message)
	if err != nil {
		return nil, nil
	}
	accessionID := uuid.NewSHA1(
		uuid.NewSHA1(uuid.NameSpaceDNS, []byte(conf.Orchestrator.ProjectFQDN)),
		body).URN()

	msg := finalize{
		Type:               "accession",
		User:               message.User,
		Filepath:           message.Filepath,
		DecryptedChecksums: message.DecryptedChecksums,
		AccessionID:        accessionID,
	}

	publish, _ := json.Marshal(&msg)

	return publish, new(finalize)
}

func mappingMessage(body []byte, conf *config.Config) ([]byte, interface{}) {
	var message finalize
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, nil
	}
	datasetID := uuid.NewSHA1(
		uuid.NewSHA1(uuid.NameSpaceDNS, []byte(conf.Orchestrator.ProjectFQDN)),
		body).URN()

	msg := mapping{
		Type:         "mapping",
		DatasetID:    datasetID,
		AccessionIDs: []string{message.AccessionID},
	}

	publish, _ := json.Marshal(&msg)

	return publish, new(mapping)
}

func releaseMessage(body []byte, conf *config.Config) ([]byte, interface{}) {
	var message finalize
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, nil
	}
	datasetID := uuid.NewSHA1(
		uuid.NewSHA1(uuid.NameSpaceDNS, []byte(conf.Orchestrator.ProjectFQDN)),
		body).URN()

	msg := mapping{
		Type:      "release",
		DatasetID: datasetID,
	}

	publish, _ := json.Marshal(&msg)

	return publish, new(mapping)
}

func validateMsg(delivered *amqp091.Delivery, mq *broker.AMQPBroker, routingKey string, durable bool, routingSchema string, publishMsg []byte, publishType interface{}) error {
	err := mq.ValidateJSON(delivered, routingSchema, publishMsg, publishType)
	if err != nil {
		return err
	}

	log.Debugf("Routing message (corr-id: %s, routingkey: %s, message: %s)",
		delivered.CorrelationId, routingKey, publishMsg)

	if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, routingKey, durable, publishMsg); err != nil {
		// TODO fix resend mechanism
		log.Errorln("We need to fix this resend stuff ...")
	}
	if err := delivered.Ack(false); err != nil {
		log.Errorf("failed to ack message for reason: %v", err)
	}

	return nil
}
