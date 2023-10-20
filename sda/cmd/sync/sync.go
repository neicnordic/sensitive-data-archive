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
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("sync")
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
	syncDestination, err := storage.NewBackend(conf.Sync)
	if err != nil {
		log.Fatal(err)
	}
	archive, err := storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}

	var key *[32]byte
	var publicKey *[32]byte
	key, err = config.GetC4GHKey()
	if err != nil {
		log.Fatal(err)
	}

	publicKey, err = config.GetC4GHPublicKey()
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

	log.Info("Starting sync service")
	var message schema.IngestionCompletion

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (ingestion-completion) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed",
					Reason:          err.Error(),
					OriginalMessage: message,
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

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)
			filePath, fileSize, err := db.GetArchived(delivered.CorrelationId)
			if err != nil {
				log.Errorf("GetArchived failed, reason: %s", err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following GetArchived error message")
				}

				continue
			}

			diskFileSize, err := archive.GetFileSize(filePath)
			if err != nil {
				log.Errorf("failed to get size info for archived file %s, reason: (%s)", filePath, err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following GetFileSize error message")
				}

				continue
			}

			if diskFileSize != int64(fileSize) {
				log.Errorf("File size in archive does not match database for archive file %s - archive size is %d, database has %d ",
					filePath, diskFileSize, fileSize,
				)
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following GetFileSize error message")
				}

				continue
			}

			file, err := archive.NewFileReader(filePath)
			if err != nil {
				log.Errorf("failed to open archived file %s, reason: (%s)", filePath, err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following open archived file error message")
				}

				continue
			}

			dest, err := syncDestination.NewFileWriter(message.FilePath)
			if err != nil {
				log.Errorf("failed to open destination file %s for writing, reason: (%s)", filePath, err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following open destination file error message")
				}

				continue
			}

			header, err := db.GetHeaderForStableID(message.AccessionID)
			if err != nil {
				log.Errorf("GetHeaderForStableID %s failed, reason: (%s)", message.AccessionID, err.Error())
			}

			log.Debug("Reencrypt header")
			pubkeyList := [][chacha20poly1305.KeySize]byte{}
			pubkeyList = append(pubkeyList, *publicKey)
			newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
			if err != nil {
				log.Errorf("failed to reencrypt the header, reason(%s)", err.Error())
				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following reencrypt header error message")
				}
			}

			_, err = dest.Write(newHeader)
			if err != nil {
				log.Errorf("failed to write the header to destination %s, reason(%s)", message.FilePath, err.Error())
			}

			// Copy the file and check is sizes match
			copiedSize, err := io.Copy(dest, file)
			if err != nil || copiedSize != int64(fileSize) {
				switch {
				case err != nil:
					log.Errorf("failed to copy the file, reason (%s)", err.Error())
				case copiedSize != int64(fileSize):
					log.Errorf("copied size does not match file size")
				}

				if err := delivered.Nack(false, false); err != nil {
					log.Errorf("failed to nack following reencrypt header error message")
				}

				continue
			}

			file.Close()
			dest.Close()

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, delivered.Body); err != nil {
				log.Errorf("failed to publish message, reason: (%s)", err.Error())

				continue
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}
		}
	}()

	<-forever
}
