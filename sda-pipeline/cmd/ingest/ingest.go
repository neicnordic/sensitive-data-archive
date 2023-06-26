// The ingest service accepts messages for files uploaded to the inbox,
// registers the files in the database with their headers, and stores them
// header-stripped in the archive storage.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"
	"sda-pipeline/internal/storage"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"

	log "github.com/sirupsen/logrus"
)

type trigger struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	Filepath           string      `json:"filepath"`
	EncryptedChecksums []checksums `json:"encrypted_checksums"`
}

// archived holds what should go in an message to inform about
// archival of files
type archived struct {
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	FileID             string      `json:"file_id"`
	ArchivePath        string      `json:"archive_path"`
	EncryptedChecksums []checksums `json:"encrypted_checksums"`
	ReVerify           bool        `json:"re_verify"`
}

type checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("ingest")
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
	version, err := db.GetVersion()
	if err != nil {
		log.Fatalf("failed to fetch database schema version: %v", err)
	}
	if version < 8 {
		log.Fatalf("database schema v8 is required")
	}
	key, err := config.GetC4GHKey()
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

	log.Info("starting ingest service")
	var message trigger

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
	mainWorkLoop:
		for delivered := range messages {
			log.Debugf("Received a message: %s", delivered.Body)

			err := mq.ValidateJSON(&delivered, "ingestion-trigger", delivered.Body, &message)
			if err != nil {
				log.Errorf("Validation of incoming message failed (corr-id: %s, error: %v)", delivered.CorrelationId, err)

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			log.Infof(
				"Received work (corr-id: %s, filepath: %s, user: %s)",
				delivered.CorrelationId, message.Filepath, message.User,
			)

			switch message.Type {
			case "cancel":
				fileUUID, err := db.GetFileID(delivered.CorrelationId)
				if err != nil || fileUUID == "" {
					log.Errorf("failed to get ID for file from message: %v", delivered.CorrelationId)

					if e := delivered.Nack(false, false); e != nil {
						log.Errorf("Failed to Nack message, reason: %v)", e)
					}

					continue
				}

				if err := db.UpdateFileStatus(fileUUID, "disabled", delivered.CorrelationId, message.User, string(delivered.Body)); err != nil {
					log.Errorf("failed to set ingestion status for file from message: %v", delivered.CorrelationId)

					if e := delivered.Nack(false, false); e != nil {
						log.Errorf("Failed to Nack message, reason: %v)", e)
					}

					continue
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message for reason: %v", err)
				}

				continue
			case "ingest":
				file, err := inbox.NewFileReader(message.Filepath)
				if err != nil {
					log.Errorf("Failed to open file to ingest (corr-id: %s, user: %s, filepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, err)
					// Nack message so the server gets notified that something is wrong. Do not requeue the message.
					if e := delivered.Nack(false, false); e != nil {
						log.Errorf("Failed to Nack message (failed to open file to ingest) (corr-id: %s, user: %s, filepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, e)
					}
					// Send the message to an error queue so it can be analyzed.
					fileError := broker.InfoError{
						Error:           "Failed to open file to ingest",
						Reason:          err.Error(),
						OriginalMessage: message,
					}
					body, _ := json.Marshal(fileError)
					if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
						log.Errorf("Failed to publish message (open file to ingest error), to error queue (corr-id: %s, user: %s, filepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, e)
					}

					// Restart on new message
					continue
				}

				fileSize, err := inbox.GetFileSize(message.Filepath)
				if err != nil {
					log.Errorf("Failed to get file size of file to inges (corr-id: %s, user: %s, filepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, err)
					// Nack message so the server gets notified that something is wrong and requeue the message.
					// Since reading the file worked, this should eventually succeed so it is ok to requeue.
					if e := delivered.Nack(false, true); e != nil {
						log.Errorf("Failed to Nack message (failed get file size) (corr-id: %s, user: %s, filepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, e)
					}
					// Send the message to an error queue so it can be analyzed.
					fileError := broker.InfoError{
						Error:           "Failed to get file size of file to ingest",
						Reason:          err.Error(),
						OriginalMessage: message,
					}
					body, _ := json.Marshal(fileError)
					if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
						log.Errorf("Failed to publish message (get file size error), to error queue (corr-id: %s, user: %s, filepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, e)
					}

					// Restart on new message
					continue
				}

				log.Infof("Got file size (corr-id: %s, user: %s, filepath: %s, filesize: %d)",
					delivered.CorrelationId, message.User, message.Filepath, fileSize)

				// Create a random uuid as file name
				archivedFile := uuid.New().String()
				dest, err := archive.NewFileWriter(archivedFile)
				if err != nil {
					log.Errorf("Failed to create archive file (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)
					// Nack message so the server gets notified that something is wrong and requeue the message.
					// NewFileWriter returns an error when the backend itself fails so this is reasonable to requeue.
					if e := delivered.Nack(false, true); e != nil {
						log.Errorf("Failed to Nack message (archive file create error) (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, archivedFile, e)
					}

					continue
				}

				fileID, err := db.RegisterFile(message.Filepath, message.User)
				if err != nil {
					log.Errorf("InsertFile failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)
				}
				err = db.UpdateFileStatus(fileID, "submitted", delivered.CorrelationId, message.User, string(delivered.Body))
				if err != nil {
					log.Errorf("failed to set ingestion status for file from message: %v", delivered.CorrelationId)
				}

				// 4MiB readbuffer, this must be large enough that we get the entire header and the first 64KiB datablock
				var bufSize int
				if bufSize = 4 * 1024 * 1024; conf.Inbox.S3.Chunksize > 4*1024*1024 {
					bufSize = conf.Inbox.S3.Chunksize
				}
				readBuffer := make([]byte, bufSize)
				hash := sha256.New()
				var bytesRead int64
				var byteBuf bytes.Buffer

				for bytesRead < fileSize {
					i, _ := io.ReadFull(file, readBuffer)
					if i == 0 {
						return
					}
					// truncate the readbuffer if the file is smaller than the buffer size
					if i < len(readBuffer) {
						readBuffer = readBuffer[:i]
					}

					bytesRead += int64(i)

					h := bytes.NewReader(readBuffer)
					if _, err = io.Copy(hash, h); err != nil {
						log.Errorf("Copy to hash failed while reading file (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

						continue mainWorkLoop
					}

					//nolint:nestif
					if bytesRead <= int64(len(readBuffer)) {
						header, err := tryDecrypt(key, readBuffer)
						if err != nil {
							log.Errorf("Trying to decrypt start of file failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
								delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

							// Nack message so the server gets notified that something is wrong. Do not requeue the message.
							if e := delivered.Nack(false, false); e != nil {
								log.Errorf("Failed to Nack message (failed decrypt file) (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
									delivered.CorrelationId, message.User, message.Filepath, archivedFile, e)
							}

							// Send the message to an error queue so it can be analyzed.
							fileError := broker.InfoError{
								Error:           "Trying to decrypt start of file failed",
								Reason:          err.Error(),
								OriginalMessage: message,
							}
							body, _ := json.Marshal(fileError)
							if e := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingError, conf.Broker.Durable, body); e != nil {
								log.Errorf("Failed to publish message (decrypt file error), to error queue (corr-id: %s, user: %s, filepath: %s, reason: %v)",
									delivered.CorrelationId, message.User, message.Filepath, e)
							}

							continue mainWorkLoop
						}
						log.Debugln("store header")
						if err := db.StoreHeader(header, fileID); err != nil {
							log.Errorf("StoreHeader failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
								delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

							continue mainWorkLoop
						}

						if _, err = byteBuf.Write(readBuffer); err != nil {
							log.Errorf("Failed to write to read buffer for header read (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
								delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

							continue mainWorkLoop
						}

						// Strip header from buffer
						h := make([]byte, len(header))
						if _, err = byteBuf.Read(h); err != nil {
							log.Errorf("Failed to read buffer for header skip (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
								delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

							continue mainWorkLoop
						}

					} else {
						if i < len(readBuffer) {
							readBuffer = readBuffer[:i]
						}
						if _, err = byteBuf.Write(readBuffer); err != nil {
							log.Errorf("Failed to write to read buffer for full read (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
								delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

							continue mainWorkLoop
						}
					}

					// Write data to file
					if _, err = byteBuf.WriteTo(dest); err != nil {
						log.Errorf("Failed to write to archive file (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
							delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

						continue mainWorkLoop
					}
				}

				file.Close()
				dest.Close()

				fileInfo := database.FileInfo{}
				fileInfo.Path = archivedFile
				fileInfo.Checksum = hash
				fileInfo.Size, err = archive.GetFileSize(archivedFile)
				if err != nil {
					log.Errorf("Couldn't get file size from archive for verification (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

					continue
				}

				log.Infof("Wrote archived file (corr-id: %s, user: %s, filepath: %s, archivepath: %s, archivedsize: %d)",
					delivered.CorrelationId, message.User, message.Filepath, archivedFile, fileInfo.Size)

				status, err := db.GetFileStatus(delivered.CorrelationId)
				if err != nil {
					log.Errorf("failed to get file status, reason: %v", err.Error())
				}
				if status == "disabled" {
					log.Infof("file with correlation ID: %s is disabled, stopping ingestion", delivered.CorrelationId)
					if err := delivered.Ack(false); err != nil {
						log.Errorf("Failed acking canceled work, reason: %v", err)
					}

					continue
				}

				if err := db.SetArchived(fileInfo, fileID, delivered.CorrelationId); err != nil {
					log.Errorf("SetArchived failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)
				}

				log.Infof("File marked as archived (corr-id: %s, user: %s, filepath: %s, archivepath: %s)",
					delivered.CorrelationId, message.User, message.Filepath, archivedFile)

				// Send message to archived
				msg := archived{
					User:        message.User,
					FilePath:    message.Filepath,
					FileID:      fileID,
					ArchivePath: archivedFile,
					EncryptedChecksums: []checksums{
						{"sha256", fmt.Sprintf("%x", hash.Sum(nil))},
					},
				}
				archivedMsg, _ := json.Marshal(&msg)

				err = mq.ValidateJSON(&delivered, "ingestion-verification", archivedMsg, new(archived))
				if err != nil {
					log.Errorf("Validation of outgoing (archived) message failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

					continue
				}

				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, conf.Broker.Durable, archivedMsg); err != nil {
					// TODO fix resend mechanism
					log.Errorf("Sending outgoing (archived) message failed (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)

					// Do not try to ACK message to make sure we have another go
					continue
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed to ack message for performed work (corr-id: %s, user: %s, filepath: %s, archivepath: %s, reason: %v)",
						delivered.CorrelationId, message.User, message.Filepath, archivedFile, err)
				}
			}
		}
	}()

	<-forever
}

// tryDecrypt tries to decrypt the start of buf.
func tryDecrypt(key *[32]byte, buf []byte) ([]byte, error) {

	log.Debugln("Try decrypting the first data block")
	a := bytes.NewReader(buf)
	b, err := streaming.NewCrypt4GHReader(a, *key, nil)
	if err != nil {
		log.Error(err)

		return nil, err

	}
	_, err = b.ReadByte()
	if err != nil {
		log.Error(err)

		return nil, err
	}

	f := bytes.NewReader(buf)
	header, err := headers.ReadHeader(f)
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return header, nil
}
