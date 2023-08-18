// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"golang.org/x/crypto/chacha20poly1305"

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

			if conf.Backup.Type != "" && conf.Archive.Type != "" {
				if err = backupFile(delivered); err != nil {
					log.Errorf("Failed to backup file with corrID: %v, reason: %v", delivered.CorrelationId, err)
					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: (%v)", err)
					}

					continue
				}
			}

			fileID, err := db.GetFileID(delivered.CorrelationId)
			if err != nil {
				log.Errorf("failed to get ID for file, reason: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			if err := db.SetAccessionID(message.AccessionID, fileID); err != nil {
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

func backupFile(delivered amqp.Delivery) error {
	log.Debug("Backup initiated")
	var checksumSha256 string
	var key *[32]byte
	var publicKey *[32]byte

	for _, checksum := range message.DecryptedChecksums {
		if checksum.Type == "sha256" {
			checksumSha256 = checksum.Value
		}
	}

	filePath, fileSize, err := db.GetArchived(message.User, message.FilePath, checksumSha256)
	if err != nil {
		return fmt.Errorf("failed to get file archive information, reason: %v", err)
	}

	// If the copy header is enabled, use the actual filepath to make backup
	// This will be used in the BigPicture backup, enabling for ingestion of the file
	if config.CopyHeader() {
		key, err = config.GetC4GHKey()
		if err != nil {
			log.Fatal(err)
		}

		publicKey, err = config.GetC4GHPublicKey()
		if err != nil {
			log.Fatal(err)
		}

		filePath = message.FilePath
	}

	// Get size on disk, will also give some time for the file to
	// appear if it has not already
	diskFileSize, err := archive.GetFileSize(filePath)
	if err != nil {
		return fmt.Errorf("failed to get size info for archived file, reason: %v", err)
	}

	if diskFileSize != int64(fileSize) {
		return fmt.Errorf("file size in archive does not match database for archive file")
	}

	file, err := archive.NewFileReader(filePath)
	if err != nil {
		return fmt.Errorf("failed to open archived file, reason: %v", err)
	}

	dest, err := backup.NewFileWriter(filePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file for writing, reason: %v", err)
	}

	fileID, err := db.GetFileID(delivered.CorrelationId)
	if err != nil {
		return fmt.Errorf("failed to get ID for file, reason: %v", err)
	}

	// Check if the header is needed
	if config.CopyHeader() {
		// Get the header from db
		header, err := db.GetHeader(fileID)
		if err != nil {
			return fmt.Errorf("failed to get header for archived file, reason: %v", err)
		}

		// Reencrypt header
		log.Debug("Reencrypt header")
		pubkeyList := [][chacha20poly1305.KeySize]byte{}
		pubkeyList = append(pubkeyList, *publicKey)
		newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
		if err != nil {
			return fmt.Errorf("failed to reencrypt the header, reason: %v)", err)
		}

		// write header to destination file
		_, err = dest.Write(newHeader)
		if err != nil {
			log.Errorf("failed to write header to file, reason: %v)", err)
		}
	}

	// Copy the file and check is sizes match
	copiedSize, err := io.Copy(dest, file)
	if err != nil || copiedSize != int64(fileSize) {
		log.Errorf("failed to copy file, reason: %v)", err)
	}

	file.Close()
	dest.Close()

	// Mark file as "backed up"
	if err := db.UpdateFileStatus(fileID, "backed up", delivered.CorrelationId, message.User, string(delivered.Body)); err != nil {
		return fmt.Errorf("MarkCompleted failed, reason: (%v)", err)
	}

	log.Debug("Backup completed")

	return nil
}
