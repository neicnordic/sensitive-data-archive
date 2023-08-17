// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
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

	log "github.com/sirupsen/logrus"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("backup")
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
	backupStorage, err := storage.NewBackend(conf.Backup)
	if err != nil {
		log.Fatal(err)
	}
	archive, err := storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}

	// we don't need crypt4gh keys if copyheader disabled
	var key *[32]byte
	var publicKey *[32]byte
	if config.CopyHeader() {
		key, err = config.GetC4GHKey()
		if err != nil {
			log.Fatal(err)
		}

		publicKey, err = config.GetC4GHPublicKey()
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

	log.Info("Starting backup service")
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

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)
			// Extract the sha256 from the message and use it for the database
			var checksumSha256 string
			for _, checksum := range message.DecryptedChecksums {
				if checksum.Type == "sha256" {
					checksumSha256 = checksum.Value
				}
			}

			var filePath string
			var fileSize int
			if filePath, fileSize, err = db.GetArchived(message.User, message.FilePath, checksumSha256); err != nil {
				log.Errorf("failed to get file archive information, reason: %v", err)

				// nack the message but requeue until we fixed the SQL retry.
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			log.Debug("Backup initiated")
			// Get size on disk, will also give some time for the file to
			// appear if it has not already
			diskFileSize, err := archive.GetFileSize(filePath)
			if err != nil {
				log.Errorf("failed to get size info for archived file, reason: %v", err)
				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			if diskFileSize != int64(fileSize) {
				log.Errorf("file size in archive does not match database for archive file")
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			file, err := archive.NewFileReader(filePath)
			if err != nil {
				log.Errorf("failed to open archived file, reason: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			// If the copy header is enabled, use the actual filepath to make backup
			// This will be used in the BigPicture backup, enabling for ingestion of the file
			if config.CopyHeader() {
				filePath = message.FilePath
			}

			dest, err := backupStorage.NewFileWriter(filePath)
			if err != nil {
				log.Errorf("failed to open backup file for writing, reason: %v", err)
				//FIXME: should it retry?
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			fileID, err := db.GetFileID(delivered.CorrelationId)
			if err != nil {
				log.Errorf("failed to get ID for file, reason: %v", err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			// Check if the header is needed
			// nolint:nestif
			if config.CopyHeader() {
				// Get the header from db
				header, err := db.GetHeader(fileID)
				if err != nil {
					log.Errorf("failed to get header for archived file, reason: %v", err)
					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: (%v)", err)
					}

					continue
				}

				// Reencrypt header
				log.Debug("Reencrypt header")
				pubkeyList := [][chacha20poly1305.KeySize]byte{}
				pubkeyList = append(pubkeyList, *publicKey)
				newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
				if err != nil {
					log.Errorf("failed to reencrypt the header, reason: %v)", err)

					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: (%v)", err)
					}
				}

				// write header to destination file
				_, err = dest.Write(newHeader)
				if err != nil {
					log.Errorf("failed to write header to file, reason: %v)", err)

					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: (%v)", err)
					}
				}
			}

			// Copy the file and check is sizes match
			copiedSize, err := io.Copy(dest, file)
			if err != nil || copiedSize != int64(fileSize) {
				log.Errorf("failed to copy file, reason: %v)", err)
				//FIXME: should it retry?
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				continue
			}

			file.Close()
			dest.Close()

			// Mark file as "backed up"
			if err := db.UpdateFileStatus(fileID, "backed up", delivered.CorrelationId, message.User, string(delivered.Body)); err != nil {
				log.Errorf("MarkCompleted failed, reason: (%v)", err)

				continue
				// this should really be hadled by the DB retry mechanism
			}

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, delivered.Body); err != nil {
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
