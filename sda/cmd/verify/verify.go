// The verify service reads and decrypts ingested files from the archive
// storage and sends accession requests.
package main

import (
	"bytes"
	"crypto/md5" // #nosec
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"

	log "github.com/sirupsen/logrus"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("verify")
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
	archive, err := storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}
	key, err := config.GetC4GHKey()
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

	log.Info("starting verify service")
	var message schema.IngestionVerification

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get messages (error: %v) ",
				err)
		}
		for delivered := range messages {
			log.Debugf("received a message (corr-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (ingestion-verifiation) failed, reason: %v", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("failed so publish message, reason: %v", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed to Ack message, reason: %v", err.Error())
				}

				// Restart on new message
				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			// If the file has been canceled by the uploader, don't spend time working on it.
			status, err := db.GetFileStatus(delivered.CorrelationId)
			if err != nil {
				log.Errorf("failed to get file status, reason: %v", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Getheader failed",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("failed so publish message, reason: %v", err.Error())
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err.Error())
				}

				continue
			}
			if status == "disabled" {
				log.Infof("file with correlation ID: %s is disabled, stopping verification", delivered.CorrelationId)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err.Error())
				}

				continue
			}

			header, err := db.GetHeader(message.FileID)
			if err != nil {
				log.Errorf("GetHeader failed for file with ID: %v, readon: %v", message.FileID, err.Error())

				// Nack message so the server gets notified that something is wrong but don't requeue the message
				if e := delivered.Nack(false, false); e != nil {
					log.Errorf("Failed to nack following getheader error message")

				}
				// store full message info in case we want to fix the db entry and retry
				infoErrorMessage := broker.InfoError{
					Error:           "Getheader failed",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)

				// Send the message to an error queue so it can be analyzed.
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("failed so publish message, reason: %v", err.Error())
				}

				continue
			}

			var file database.FileInfo

			file.Size, err = archive.GetFileSize(message.ArchivePath)

			if err != nil {
				log.Errorf("Failed to get archived file size, reson: %v", err.Error())

				continue
			}

			archiveFileHash := sha256.New()
			f, err := archive.NewFileReader(message.ArchivePath)
			if err != nil {
				log.Errorf("Failed to open archived file, reson: %v ", err.Error())

				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Failed to open archived file",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%v)", err.Error())
				}

				// Restart on new message
				continue
			}

			hr := bytes.NewReader(header)
			// Feed everything read from the archive file to archiveFileHash
			mr := io.MultiReader(hr, io.TeeReader(f, archiveFileHash))

			c4ghr, err := streaming.NewCrypt4GHReader(mr, *key, nil)
			if err != nil {
				log.Errorf("Failed to open c4gh decryptor stream, reson: %v", err.Error())

				continue
			}

			md5hash := md5.New() // #nosec
			sha256hash := sha256.New()

			stream := io.TeeReader(c4ghr, md5hash)

			if file.DecryptedSize, err = io.Copy(sha256hash, stream); err != nil {
				log.Errorf("failed to copy decrypted data, reson: %v", err.Error())

				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Failed to verify archived file",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
					log.Errorf("Failed to publish error message: %v", e)
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed to ack message: %v", err.Error())
				}

				continue
			}

			file.Checksum = archiveFileHash
			file.DecryptedChecksum = sha256hash

			//nolint:nestif
			if !message.ReVerify {

				c := schema.IngestionAccessionRequest{
					User:     message.User,
					FilePath: message.FilePath,
					DecryptedChecksums: []schema.Checksums{
						{Type: "sha256", Value: fmt.Sprintf("%x", sha256hash.Sum(nil))},
						{Type: "md5", Value: fmt.Sprintf("%x", md5hash.Sum(nil))},
					},
				}

				verifiedMessage, _ := json.Marshal(&c)

				err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession-request.json", conf.Broker.SchemasPath), verifiedMessage)

				if err != nil {
					log.Errorf("Validation of outgoing (ingestion-accession-request) failed, reason: %v", err.Error())

					// Logging is in ValidateJSON so just restart on new message
					continue
				}
				status, err := db.GetFileStatus(delivered.CorrelationId)
				if err != nil {
					log.Errorf("failed to get file status, reason: %v", err.Error())
					// Send the message to an error queue so it can be analyzed.
					infoErrorMessage := broker.InfoError{
						Error:           "Getheader failed",
						Reason:          err.Error(),
						OriginalMessage: message,
					}

					body, _ := json.Marshal(infoErrorMessage)
					if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); e != nil {
						log.Errorf("failed so publish message, reason: %v", err.Error())
					}

					if err := delivered.Ack(false); err != nil {
						log.Errorf("Failed acking canceled work, reason: %v", err.Error())
					}

					continue
				}
				if status == "disabled" {
					log.Infof("file with correlation ID: %s is disabled, stopping verification", delivered.CorrelationId)
					if err := delivered.Ack(false); err != nil {
						log.Errorf("Failed acking canceled work, reason: %v", err.Error())
					}

					continue
				}

				// Mark file as "COMPLETED"
				if err := db.MarkCompleted(file, message.FileID, delivered.CorrelationId); err != nil {
					log.Errorf("MarkCompleted failed, reason: (%v)", err.Error())

					continue
					// this should really be hadled by the DB retry mechanism
				}

				// Send message to verified queue
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, verifiedMessage); err != nil {
					// TODO fix resend mechanism
					log.Errorf("failed to publish message, reason: (%v)", err.Error())

					continue
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%v)", err.Error())
				}
			}
		}
	}()

	<-forever
}
