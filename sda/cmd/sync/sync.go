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

var (
	key, publicKey           *[32]byte
	db                       *database.SDAdb
	archive, syncDestination storage.Backend
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
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	syncDestination, err = storage.NewBackend(conf.Sync)
	if err != nil {
		log.Fatal(err)
	}
	archive, err = storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}

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
	var message schema.DatasetMapping

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

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

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			for _, aID := range message.AccessionIDs {
				if err := syncFiles(aID); err != nil {
					log.Errorf("failed to sync archived file %s, reason: (%s)", aID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetFileSize error message")
					}

					continue
				}
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}
		}
	}()

	<-forever
}

func syncFiles(stableID string) error {
	log.Debugf("syncing file %s", stableID)
	inboxPath, err := db.GetInboxPath(stableID)
	if err != nil {
		return fmt.Errorf("failed to get inbox path for file with stable ID: %s", stableID)
	}

	archivePath, err := db.GetArchivePath(stableID)
	if err != nil {
		return fmt.Errorf("failed to get archive path for file with stable ID: %s", stableID)
	}

	fileSize, err := archive.GetFileSize(archivePath)
	if err != nil {
		return err
	}

	file, err := archive.NewFileReader(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	dest, err := syncDestination.NewFileWriter(inboxPath)
	if err != nil {
		return err
	}
	defer dest.Close()

	header, err := db.GetHeaderForStableID(stableID)
	if err != nil {
		return err
	}

	pubkeyList := [][chacha20poly1305.KeySize]byte{}
	pubkeyList = append(pubkeyList, *publicKey)
	newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
	if err != nil {
		return err
	}

	_, err = dest.Write(newHeader)
	if err != nil {
		return err
	}

	// Copy the file and check is sizes match
	copiedSize, err := io.Copy(dest, file)
	if err != nil || copiedSize != int64(fileSize) {
		switch {
		case copiedSize != int64(fileSize):
			return fmt.Errorf("copied size does not match file size")
		default:
			return err
		}
	}

	return nil
}
