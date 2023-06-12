// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"encoding/json"
	"errors"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"
	"sda-pipeline/internal/storage"

	log "github.com/sirupsen/logrus"
)

type message struct {
	Type         string   `json:"type"`
	DatasetID    string   `json:"dataset_id"`
	AccessionIDs []string `json:"accession_ids"`
}

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("mapper")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.NewDB(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	inbox, err := storage.NewBackend(conf.Inbox)
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
	var mappings message

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get message from mq (error: %v)", err)
		}
		for delivered := range messages {
			log.Debugf("received a message: %s", delivered.Body)

			schema, err := schemaFromDatasetOperation(delivered.Body)

			if err != nil {
				log.Errorf(err.Error())

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message: %v", err)
				}
				if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, delivered.Body); err != nil {
					log.Errorf("failed to send error message: %v", err)
				}

				continue
			}

			err = mq.ValidateJSON(&delivered, schema, delivered.Body, &mappings)
			if err != nil {
				log.Errorf("Failed to validate message for work "+
					"(corr-id: %s, "+
					"message: %s, "+
					"error: %v)",
					delivered.CorrelationId,
					delivered.Body,
					err)

				continue
			}

			if err := json.Unmarshal(delivered.Body, &mappings); err != nil {
				log.Errorf("Failed to unmarshal message for work "+
					"(corr-id: %s, "+
					"message: %s, "+
					"error: %v)",
					delivered.CorrelationId,
					delivered.Body,
					err)

				continue
			}

			switch mappings.Type {
			case "mapping":

				log.Debug("Mapping type operation, mapping files to dataset")

				if err := db.MapFilesToDataset(mappings.DatasetID, mappings.AccessionIDs); err != nil {
					log.Errorf("MapFilesToDataset failed  "+
						"(corr-id: %s, "+
						"datasetid: %s, "+
						"accessionids: %v, "+
						"error: %v)",
						delivered.CorrelationId,
						mappings.DatasetID,
						mappings.AccessionIDs,
						err)

					// Nack message so the server gets notified that something is wrong and requeue the message
					if e := delivered.Nack(false, true); e != nil {
						log.Errorf("Failed to nack message on mapping files to dataset) "+
							"(corr-id: %s, "+
							"datasetid: %s, "+
							"accessionid: %s, "+
							"reason: %v)",
							delivered.CorrelationId,
							mappings.DatasetID,
							mappings.AccessionIDs,
							e)
					}

					continue
				}

				for _, aID := range mappings.AccessionIDs {
					log.Infof(
						"Mapped file to dataset (corr-id: %s, datasetid: %s, accessionid: %s)",
						delivered.CorrelationId, mappings.DatasetID, aID,
					)

					filePath, err := db.GetInboxPath(aID)
					if err != nil {
						log.Errorf("failed to get inbox path for file with stable ID: %v", aID)
					}
					err = inbox.RemoveFile(filePath)
					if err != nil {
						log.Errorf("Remove file from inbox failed, reason: %v", err)
					}
				}
			case "release":

				log.Debug("Release type operation, marking files as ready")

				if err := db.UpdateDatasetEvent(mappings.DatasetID, "ready", delivered.CorrelationId, "mapper"); err != nil {
					log.Errorf("MarkReady failed  "+
						"(corr-id: %s, "+
						"datasetid: %s, "+
						"accessionids: %v, "+
						"error: %v)",
						delivered.CorrelationId,
						mappings.DatasetID,
						mappings.AccessionIDs,
						err)

					// Nack message so the server gets notified that something is wrong and requeue the message
					if e := delivered.Nack(false, true); e != nil {
						log.Errorf("Failed to nack message on marking ready the files in the dataset) "+
							"(corr-id: %s, "+
							"datasetid: %s, "+
							"accessionid: %s, "+
							"reason: %v)",
							delivered.CorrelationId,
							mappings.DatasetID,
							mappings.AccessionIDs,
							e)
					}

					continue
				}

			case "deprecate":

				log.Debug("Deprecate type operation, marking files as disabled")

				// in the database this will translate to disabling of a file
				if err := db.UpdateDatasetEvent(mappings.DatasetID, "disabled", delivered.CorrelationId, "mapper"); err != nil {
					log.Errorf("MarkReady failed  "+
						"(corr-id: %s, "+
						"datasetid: %s, "+
						"accessionids: %v, "+
						"error: %v)",
						delivered.CorrelationId,
						mappings.DatasetID,
						mappings.AccessionIDs,
						err)

					// Nack message so the server gets notified that something is wrong and requeue the message
					if e := delivered.Nack(false, true); e != nil {
						log.Errorf("Failed to nack message on marking ready the files in the dataset) "+
							"(corr-id: %s, "+
							"datasetid: %s, "+
							"accessionid: %s, "+
							"reason: %v)",
							delivered.CorrelationId,
							mappings.DatasetID,
							mappings.AccessionIDs,
							e)
					}

					continue
				}
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("Failed to ack message for work "+
					"(corr-id: %s, "+
					"datasetid: %s, "+
					"accessionids: %v, "+
					"error: %v)",
					delivered.CorrelationId,
					mappings.DatasetID,
					mappings.AccessionIDs,
					err)
			}
		}
	}()

	<-forever
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
