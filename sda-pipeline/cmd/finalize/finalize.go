// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"encoding/json"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"

	log "github.com/sirupsen/logrus"
)

// finalize struct that holds the json message data
type finalize struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []checksums `json:"decrypted_checksums"`
}

// Checksums is struct for the checksum type and value
type checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Completed is struct holding the full message data
type completed struct {
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []checksums `json:"decrypted_checksums"`
}

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
	db, err := database.NewDB(conf.Database)
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
	var message finalize

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := mq.ValidateJSON(&delivered,
				"ingestion-accession",
				delivered.Body,
				&message)

			if err != nil {
				log.Errorf("Validation of incoming message failed "+
					"(corr-id: %s, error: %v)",
					delivered.CorrelationId,
					err)

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			log.Infof("Received work (corr-id: %s, "+
				"filepath: %s, "+
				"user: %s, "+
				"accessionid: %s, "+
				"decryptedChecksums: %v)",
				delivered.CorrelationId,
				message.Filepath,
				message.User,
				message.AccessionID,
				message.DecryptedChecksums)

			// If the file has been canceled by the uploader, don't spend time working on it.
			status, err := db.GetFileStatus(delivered.CorrelationId)
			if err != nil {
				log.Errorf("failed to get file status, reason: %v", err.Error())
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

			c := completed{
				User:               message.User,
				Filepath:           message.Filepath,
				AccessionID:        message.AccessionID,
				DecryptedChecksums: message.DecryptedChecksums,
			}

			completeMsg, _ := json.Marshal(&c)

			err = mq.ValidateJSON(&delivered,
				"ingestion-completion",
				completeMsg,
				new(completed))

			if err != nil {
				log.Errorf("Validation of outgoing message failed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				continue
			}

			accessionIDExists, err := db.CheckAccessionIDExists(message.AccessionID)
			if err != nil {
				log.Errorf("CheckAccessionIdExists failed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of checking accession id exists failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.Filepath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)

				}

				continue
			}

			if accessionIDExists {

				log.Infof("Seems accession ID already exists (corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums)

				// Send the message to an error queue so it can be analyzed.
				fileError := broker.InfoError{
					Error:           "There is a conflict regarding the file accessionID",
					Reason:          "The Accession ID already exists in the database, skipping marking it ready.",
					OriginalMessage: message,
				}
				body, _ := json.Marshal(fileError)

				// Send the message to an error queue so it can be analyzed.
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
					log.Errorf("Failed to publish conflict in accessionID error message "+
						"(corr-id: %s, user: %s, filepath: %s, accessionID: %s, decryptedchecksums: %v, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.Filepath,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				// Nack message so the server gets notified that something is wrong and don't requeue the message
				if e := delivered.Nack(false, false); e != nil {
					log.Errorf("Failed to NAck because of sending error failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.Filepath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)

				}

				continue

			}

			if err := db.SetAccessionID(message.AccessionID, message.User, message.Filepath, checksumSha256); err != nil {
				log.Errorf("SetAccessionID failed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of SetAccessionID failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.Filepath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)

				}

				continue
			}

			log.Infof("Set accession id for file "+
				"(corr-id: %s, "+
				"filepath: %s, "+
				"user: %s, "+
				"accessionid: %s, "+
				"decryptedChecksums: %v)",
				delivered.CorrelationId,
				message.Filepath,
				message.User,
				message.AccessionID,
				message.DecryptedChecksums)

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, conf.Broker.Durable, completeMsg); err != nil {
				// TODO fix resend mechanism
				log.Errorf("Failed to send message for completed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				// Restart loop, do not ack
				continue
			}

			if err := delivered.Ack(false); err != nil {

				log.Errorf("Failed to ack message after work completed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.Filepath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

			}

		}
	}()

	<-forever
}
