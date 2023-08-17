// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"

	log "github.com/sirupsen/logrus"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("finalize")
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

	log.Info("Starting finalize service")
	var message schema.IngestionAccession

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (ingestion-accession) failed, reason: %v ", err)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
				}

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)
			// If the file has been canceled by the uploader, don't spend time working on it.
			status, err := db.GetFileStatus(delivered.CorrelationId)
			if err != nil {
				log.Errorf("failed to get file status, reason: %v", err)
			}
			if status == "disabled" {
				log.Infof("file with correlation ID: %s is disabled, stopping work", delivered.CorrelationId)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
				}

				continue
			}

			// Extract the sha256 from the message and use it for the database
			var checksumSha256 string
			for _, checksum := range message.DecryptedChecksums {
				if checksum.Type == "sha256" {
					checksumSha256 = checksum.Value
				}
			}

			c := schema.IngestionCompletion{
				User:               message.User,
				FilePath:           message.FilePath,
				AccessionID:        message.AccessionID,
				DecryptedChecksums: message.DecryptedChecksums,
			}
			completeMsg, _ := json.Marshal(&c)
			err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", conf.Broker.SchemasPath), completeMsg)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, reason: (%v)", err)

				continue
			}

			accessionIDExists, err := db.CheckAccessionIDExists(message.AccessionID)
			if err != nil {
				log.Errorf("CheckAccessionIdExists failed, reason: %v ", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			if accessionIDExists {
				log.Debugf("Seems accession ID already exists (corr-id: %s, accessionid: %s", delivered.CorrelationId, message.AccessionID)
				// Send the message to an error queue so it can be analyzed.
				fileError := broker.InfoError{
					Error:           "There is a conflict regarding the file accessionID",
					Reason:          "The Accession ID already exists in the database, skipping marking it ready.",
					OriginalMessage: message,
				}
				body, _ := json.Marshal(fileError)

				// Send the message to an error queue so it can be analyzed.
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("failed to publish message, reason: (%v)", err)
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%v)", err)
				}

				continue

			}

			if err := db.SetAccessionID(message.AccessionID, message.User, message.FilePath, checksumSha256); err != nil {
				log.Errorf("Failed to set accessionID for file with corrID: %v, reason: %v", delivered.CorrelationId, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			// Mark file as "ready"
			if err := db.UpdateFileStatus(fileID, "ready", delivered.CorrelationId, "finalize", string(delivered.Body)); err != nil {
				log.Errorf("set status ready failed, reason: (%v)", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			c := schema.IngestionCompletion{
				User:               message.User,
				FilePath:           message.FilePath,
				AccessionID:        message.AccessionID,
				DecryptedChecksums: message.DecryptedChecksums,
			}
			completeMsg, _ := json.Marshal(&c)
			err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", conf.Broker.SchemasPath), completeMsg)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, reason: (%v)", err)

				continue
			}

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, completeMsg); err != nil {
				// TODO fix resend mechanism
				log.Errorf("failed to publish message, reason: (%v)", err)

				continue
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%v)", err)
			}
		}
	}()

	<-forever
}
