package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("sync-ctrl")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.NewSDAdb(conf.Database)
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

	log.Info("Starting sync control service")

	go handleDatasetMsg(conf, db, mq)
	go handleAccessionMsg(conf, db, mq)

	<-forever
}

func handleDatasetMsg(conf *config.Config, db *database.SDAdb, mq *broker.AMQPBroker) {
	messages, err := mq.GetMessages(conf.Broker.Queue)
	if err != nil {
		log.Fatal(err)
	}

	for delivered := range messages {
		log.Debugf("Received a message (message: %s)", delivered.Body)
		var msgType struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(delivered.Body, &msgType)

		switch msgType.Type {
		case "deprectate":
			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-deprecate.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (dataset-mapping) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed in sync service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			message := schema.DatasetMapping{}
			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			if !strings.HasPrefix(message.DatasetID, conf.SyncCtrl.CenterPrefix) {
				log.Infof("skipping external dataset: %s", message.DatasetID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			for count := 1; count <= database.RetryTimes; count++ {
				err := mq.SendMessage("", conf.Broker.Exchange, "sync_files", delivered.Body)
				if err == nil {
					break
				}

				if count == database.RetryTimes {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following send message failure")
					}

					continue
				}

				time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
			}
		case "mapping":
			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (dataset-mapping) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed in sync service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			message := schema.DatasetMapping{}
			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			if !strings.HasPrefix(message.DatasetID, conf.SyncCtrl.CenterPrefix) {
				log.Infof("skipping external dataset: %s", message.DatasetID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			for _, aID := range message.AccessionIDs {
				log.Debugf("creating message for file %s", aID)
				fileData, err := db.GetSyncData(aID)
				if err != nil {
					log.Errorf("failed to create message for file %s, reason: (%s)", aID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetSyncData error message")
					}

					continue
				}

				file := schema.SyncFileData{
					AccessionID:       aID,
					DecryptedChecksum: fileData.Checksum,
					FilePath:          fileData.FilePath,
					Type:              "sync",
					User:              fileData.User,
				}

				fileMsg, _ := json.Marshal(file)
				if err := schema.ValidateJSON(fmt.Sprintf("%s/sync-file.json", conf.Broker.SchemasPath), fileMsg); err != nil {
					log.Errorf("failed to create message for file %s, reason: (%s)", aID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following message validation")
					}

					continue
				}

				for count := 1; count <= database.RetryTimes; count++ {
					err := mq.SendMessage("", conf.Broker.Exchange, "sync_files", fileMsg)
					if err == nil {
						break
					}

					if count == database.RetryTimes {
						log.Errorf("failed to publish message, reason: (%s)", err.Error())
						if err := delivered.Nack(false, false); err != nil {
							log.Errorf("failed to nack following send message failure")
						}

						continue
					}

					time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
				}
			}
		case "release":
			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (dataset-mapping) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed in sync service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			message := schema.DatasetMapping{}
			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			if !strings.HasPrefix(message.DatasetID, conf.SyncCtrl.CenterPrefix) {
				log.Infof("skipping external dataset: %s", message.DatasetID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			for count := 1; count <= database.RetryTimes; count++ {
				err := mq.SendMessage("", conf.Broker.Exchange, "sync_files", delivered.Body)
				if err == nil {
					break
				}

				if count == database.RetryTimes {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following send message failure")
					}

					continue
				}

				time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
			}
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: (%s)", err.Error())
		}
	}
}

func handleAccessionMsg(conf *config.Config, db *database.SDAdb, mq *broker.AMQPBroker) {
	messages, err := mq.GetMessages("sync_accession")
	if err != nil {
		log.Fatal(err)
	}

	for delivered := range messages {
		log.Debugf("Received a message (message: %s)", delivered.Body)

		message := schema.IngestionAccession{}
		// we unmarshal the message in the validation step so this is safe to do
		_ = json.Unmarshal(delivered.Body, &message)

		if strings.HasPrefix(message.AccessionID, conf.SyncCtrl.CenterPrefix) {
			log.Infof("skipping local dataset: %s", message.AccessionID)
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue
		}

		corrID, err := db.GetCorrID(message.User, message.FilePath)
		if err != nil {
			log.Infof("skipping local dataset: %s", message.AccessionID)
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue
		}

		status, err := db.GetFileStatus(corrID)
		if err != nil {
			log.Infof("skipping local dataset: %s", message.AccessionID)
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue
		}

		if status != "verified" {
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue

		}

		for count := 1; count <= database.RetryTimes; count++ {
			err := mq.SendMessage(corrID, conf.Broker.Exchange, "accession", delivered.Body)
			if err == nil {
				break
			}

			if count == database.RetryTimes {
				log.Errorf("failed to publish message, reason: (%s)", err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following send message failure")
				}

				continue
			}

			time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: (%s)", err.Error())
		}
	}
}
