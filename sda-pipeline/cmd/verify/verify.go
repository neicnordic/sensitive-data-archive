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

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"
	"sda-pipeline/internal/storage"

	"github.com/neicnordic/crypt4gh/streaming"

	log "github.com/sirupsen/logrus"
)

// Message struct that holds the json message data
type message struct {
	FilePath           string      `json:"filepath"`
	User               string      `json:"user"`
	FileID             int         `json:"file_id"`
	ArchivePath        string      `json:"archive_path"`
	EncryptedChecksums []checksums `json:"encrypted_checksums"`
	ReVerify           bool        `json:"re_verify"`
}

// Verified is struct holding the full message data
type verified struct {
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	DecryptedChecksums []checksums `json:"decrypted_checksums"`
}

// Checksums is struct for the checksum type and value
type checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

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
	db, err := database.NewDB(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	archive, err := storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}
	inbox, err := storage.NewBackend(conf.Inbox)
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

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get messages (error: %v) ",
				err)
		}
		for delivered := range messages {
			var message message
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := mq.ValidateJSON(&delivered, "ingestion-verification", delivered.Body, &message)

			if err != nil {
				log.Errorf("Validation (ingestion-verifiation) of incoming message failed "+
					"(corr-id: %s, error: %v, message: %s)",
					delivered.CorrelationId,
					err,
					delivered.Body)

				// Restart on new message
				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			log.Infof("Received work "+
				"(corr-id: %s, user: %s, filepath: %s, fileid: %d, archivepath: %s, encryptedchecksums: %v, reverify: %t)",
				delivered.CorrelationId,
				message.User,
				message.FilePath,
				message.FileID,
				message.ArchivePath,
				message.EncryptedChecksums,
				message.ReVerify)

			header, err := db.GetHeader(message.FileID)
			if err != nil {
				log.Errorf("GetHeader failed "+
					"(corr-id: %s, user: %s, filepath: %s, fileid: %d, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.FileID,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					err)

				// Nack message so the server gets notified that something is wrong but don't requeue the message
				if e := delivered.Nack(false, false); e != nil {
					log.Errorf("Failed to nack following getheader error message "+
						"(corr-id: %s, user: %s, filepath: %s, fileid: %d, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.FileID,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						e)

				}
				// store full message info in case we want to fix the db entry and retry
				infoErrorMessage := broker.InfoError{
					Error:           "Getheader failed",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)

				// Send the message to an error queue so it can be analyzed.
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
					log.Errorf("Failed to publish getheader error message "+
						"(corr-id: %s, user: %s, filepath: %s, fileid: %d, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.FileID,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						e)
				}

				continue
			}

			var file database.FileInfo

			file.Size, err = archive.GetFileSize(message.ArchivePath)

			if err != nil {
				log.Errorf("Failed to get archived file size "+
					"(corr-id: %s, user: %s, filepath: %s, fileid: %d, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.FileID,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					err)

				continue
			}

			log.Infof("Got archived file size "+
				"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, archivedsize: %d)",
				delivered.CorrelationId,
				message.User,
				message.FilePath,
				message.ArchivePath,
				message.EncryptedChecksums,
				message.ReVerify,
				file.Size)

			archiveFileHash := sha256.New()

			f, err := archive.NewFileReader(message.ArchivePath)
			if err != nil {
				log.Errorf("Failed to open archived file "+
					"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					err)

				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Failed to open archived file",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {

					log.Errorf("Failed to publish file open error message "+
						"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						e)

				}

				// Restart on new message
				continue
			}

			hr := bytes.NewReader(header)
			// Feed everything read from the archive file to archiveFileHash
			mr := io.MultiReader(hr, io.TeeReader(f, archiveFileHash))

			c4ghr, err := streaming.NewCrypt4GHReader(mr, *key, nil)
			if err != nil {
				log.Errorf("Failed to open c4gh decryptor stream "+
					"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					err)

				continue
			}

			md5hash := md5.New() // #nosec
			sha256hash := sha256.New()

			stream := io.TeeReader(c4ghr, md5hash)

			if file.DecryptedSize, err = io.Copy(sha256hash, stream); err != nil {
				log.Errorf("Failed to copy decrypted data to hash stream "+
					"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					err)

				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Failed to verify archived file",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
					log.Errorf("Failed to publish error message: %v", e)
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed to ack message: %v", err)
				}

				continue
			}

			file.Checksum = archiveFileHash
			file.DecryptedChecksum = sha256hash

			log.Infof("Calculated decrypted hash "+
				"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, "+
				"encryptedchecksums: %v, reverify: %t, decryptedsize: %d, "+
				"decryptedchecksum: %x)",
				delivered.CorrelationId,
				message.User,
				message.FilePath,
				message.ArchivePath,
				message.EncryptedChecksums,
				message.ReVerify,
				file.DecryptedSize,
				file.DecryptedChecksum.Sum(nil))

			//nolint:nestif
			if !message.ReVerify {

				c := verified{
					User:     message.User,
					FilePath: message.FilePath,
					DecryptedChecksums: []checksums{
						{"sha256", fmt.Sprintf("%x", sha256hash.Sum(nil))},
						{"md5", fmt.Sprintf("%x", md5hash.Sum(nil))},
					},
				}

				verifiedMessage, _ := json.Marshal(&c)

				err = mq.ValidateJSON(&delivered,
					"ingestion-accession-request",
					verifiedMessage,
					new(verified))

				if err != nil {
					log.Errorf("Validation (ingestion-accession-request) of outgoing message failed "+
						"(corr-id: %s, error: %v, message: %s)",
						delivered.CorrelationId,
						err,
						verifiedMessage)

					// Logging is in ValidateJSON so just restart on new message
					continue
				}

				// Mark file as "COMPLETED"
				if e := db.MarkCompleted(file, message.FileID); e != nil {
					log.Errorf("MarkCompleted failed "+
						"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						e)

					continue
					// this should really be hadled by the DB retry mechanism
				}

				log.Infof("File marked completed "+
					"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, decryptedchecksum: %x)",
					delivered.CorrelationId,
					message.User,
					message.FilePath,
					message.ArchivePath,
					message.EncryptedChecksums,
					message.ReVerify,
					file.DecryptedChecksum.Sum(nil))

				// Send message to verified queue

				if err := mq.SendMessage(delivered.CorrelationId,
					conf.Broker.Exchange,
					conf.Broker.RoutingKey,
					conf.Broker.Durable,
					verifiedMessage); err != nil {
					// TODO fix resend mechanism

					log.Errorf("Sending of message failed "+
						"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						err)

					continue
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking completed work"+
						"(corr-id: %s, user: %s, filepath: %s, archivepath: %s, encryptedchecksums: %v, reverify: %t, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						message.ArchivePath,
						message.EncryptedChecksums,
						message.ReVerify,
						err)
				}

				// At the end we try to remove file from inbox
				// In case of error we send a message to error queue to track it
				// we don't need to force removing the file
				err = inbox.RemoveFile(message.FilePath)
				if err != nil {
					log.Errorf("Remove file from inbox failed "+
						"(corr-id: %s, user: %s, filepath: %s, reason: %v)",
						delivered.CorrelationId,
						message.User,
						message.FilePath,
						err)

					// Send the message to an error queue so it can be analyzed.
					fileError := broker.InfoError{
						Error:           "RemoveFile failed",
						Reason:          err.Error(),
						OriginalMessage: message,
					}
					body, _ := json.Marshal(fileError)
					if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
						log.Errorf("Failed to publish message (remove file error), to error queue "+
							"(corr-id: %s, user: %s, filepath: %s, reason: %v)",
							delivered.CorrelationId,
							message.User,
							message.FilePath,
							e)
					}

					continue
				}
				log.Debugf("Removed file from inbox: %s", message.FilePath)

			}

		}
	}()

	<-forever
}
