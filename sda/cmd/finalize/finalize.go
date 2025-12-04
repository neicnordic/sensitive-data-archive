// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

var db *database.SDAdb
var archive, backup storage.Backend
var conf *config.Config
var err error
var message schema.IngestionAccession

func main() {
	forever := make(chan bool)
	conf, err = config.NewConfig("finalize")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}

	if conf.Backup.Type != "" && conf.Archive.Type != "" {
		log.Debugln("initiating storage backends")
		backup, err = storage.NewBackend(conf.Backup)
		if err != nil {
			log.Fatal(err)
		}
		archive, err = storage.NewBackend(conf.Archive)
		if err != nil {
			log.Fatal(err)
		}
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
	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (correlation-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (ingestion-accession) failed, correlation-id: %s, reason: %v ", delivered.CorrelationId, err)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
				}

				continue
			}

			fileID := delivered.CorrelationId
			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)
			// If the file has been canceled by the uploader, don't spend time working on it.
			status, err := db.GetFileStatus(fileID)
			if err != nil {
				log.Errorf("failed to get file status, file-id: %s, reason: %v", fileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}

				continue
			}

			switch status {
			case "disabled":
				log.Infof("file with file-id: %s is disabled, aborting work", fileID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
				}

				continue

			case "verified", "enabled":
			case "ready":
				log.Infof("File with file-id: %s is already marked as ready.", fileID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking message, reason: %v", err)
				}

				continue
			default:
				log.Warnf("file with file-id: %s is not verified yet, aborting work", fileID)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
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
				log.Errorf("Validation of outgoing message ingestion-completion failed, reason: (%v). Message body: %s\n", err, string(completeMsg))

				continue
			}

			accessionIDExists, err := db.CheckAccessionIDExists(message.AccessionID, fileID)
			if err != nil {
				log.Errorf("CheckAccessionIdExists failed, file-id: %s, reason: %v ", fileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}

				continue
			}

			switch accessionIDExists {
			case "duplicate":
				log.Errorf("accession ID already exists in the system, file-id: %s, accession-id: %s\n", fileID, message.AccessionID)
				// Send the message to an error queue so it can be analyzed.
				fileError := broker.InfoError{
					Error:           "There is a conflict regarding the file accessionID",
					Reason:          "The Accession ID already exists in the database, skipping marking it ready.",
					OriginalMessage: message,
				}
				body, _ := json.Marshal(fileError)

				// Send the message to an error queue so it can be analyzed.
				if e := mq.SendMessage(fileID, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("failed to publish message, reason: %v", err)
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: %v", err)
				}

				continue
			case "same":
				log.Infof("file already has a stable ID, marking it as ready, file-id: %s", fileID)
			default:
				if conf.Backup.Type != "" && conf.Archive.Type != "" {
					if err = backupFile(delivered); err != nil {
						log.Errorf("failed to backup file, file-id: %s, reason: %v", fileID, err)
						if err := delivered.Nack(false, true); err != nil {
							log.Errorf("failed to Nack message, reason: %v", err)
						}

						continue
					}
				}

				if err := db.SetAccessionID(message.AccessionID, fileID); err != nil {
					log.Errorf("failed to set accessionID for file, file-id: %s, reason: %v", fileID, err)
					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: %v", err)
					}

					continue
				}
			}

			// Mark file as "ready"
			if err := db.UpdateFileEventLog(fileID, "ready", "finalize", "{}", string(delivered.Body)); err != nil {
				log.Errorf("set status ready failed, file-id: %s, reason: %v", fileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}

				continue
			}

			if err := mq.SendMessage(fileID, conf.Broker.Exchange, conf.Broker.RoutingKey, completeMsg); err != nil {
				log.Errorf("failed to publish message, reason: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}

				continue
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: %v", err)
			}
		}
	}()

	<-forever
}

func backupFile(delivered amqp.Delivery) error {
	log.Debug("Backup initiated")
	fileID := delivered.CorrelationId

	filePath, fileSize, err := db.GetArchived(fileID)
	if err != nil {
		return fmt.Errorf("failed to get file archive information, reason: %v", err)
	}

	// Get size on disk, will also give some time for the file to appear if it has not already
	diskFileSize, err := archive.GetFileSize(filePath, false)
	if err != nil {
		return fmt.Errorf("failed to get size info for archived file, reason: %v", err)
	}

	if diskFileSize != int64(fileSize) {
		return fmt.Errorf("archive file size does not match registered file size, (disk size: %d, db size: %d)", diskFileSize, fileSize)
	}

	file, err := archive.NewFileReader(filePath)
	if err != nil {
		return fmt.Errorf("failed to open archived file, reason: %v", err)
	}
	defer file.Close()

	dest, err := backup.NewFileWriter(filePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file for writing, reason: %v", err)
	}
	defer dest.Close()

	// Copy the file and check is sizes match
	copiedSize, err := io.Copy(dest, file)
	if err != nil || copiedSize != int64(fileSize) {
		log.Errorf("failed to copy file, reason: %v)", err)
	}

	// Mark file as "backed up"
	if err := db.UpdateFileEventLog(fileID, "backed up", "finalize", "{}", string(delivered.Body)); err != nil {
		return fmt.Errorf("UpdateFileEventLog failed, reason: (%v)", err)
	}

	log.Debug("Backup completed")

	return nil
}
