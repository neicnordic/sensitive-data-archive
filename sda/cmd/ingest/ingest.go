// The ingest service accepts messages for files uploaded to the inbox,
// registers the files in the database with their headers, and stores them
// header-stripped in the archive storage.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"

	log "github.com/sirupsen/logrus"
)

type Ingest struct {
	Archive        storage.Backend
	ArchiveKeyList []*[32]byte
	Conf           *config.Config
	DB             *database.SDAdb
	Inbox          storage.Backend
	MQ             *broker.AMQPBroker
}

func (app *Ingest) main() {
	var err error
	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	// Create a function to handle panic and exit gracefully
	defer func() {
		if err := recover(); err != nil {
			log.Fatal("Could not recover, exiting")
		}
	}()

	forever := make(chan bool)

	app.Conf, err = config.NewConfig("ingest")
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	app.MQ, err = broker.NewMQ(app.Conf.Broker)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	app.DB, err = database.NewSDAdb(app.Conf.Database)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	if app.DB.Version < 8 {
		log.Error("database schema v8 is required")
		sigc <- syscall.SIGINT
		panic(err)
	}
	app.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	app.Archive, err = storage.NewBackend(app.Conf.Archive)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	app.Inbox, err = storage.NewBackend(app.Conf.Inbox)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	defer app.MQ.Channel.Close()
	defer app.MQ.Connection.Close()
	defer app.DB.Close()

	go func() {
		connError := app.MQ.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := app.MQ.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("starting ingest service")

	go func() {
		start := time.Now()
		for i := 1; i > 0; i++ {
			h, err := app.DB.ListKeyHashes()
			if err != nil {
				log.Errorln(err.Error())
			}
			if len(h) != 0 {
				break
			}

			time.Sleep(time.Duration(30 * time.Second))
			if time.Since(start).Seconds() >= float64(300) {
				log.Errorln("no crypt4gh key hash registered, restarting")
				forever <- false
			}
			log.Errorln("no crypt4gh key hash registered")
		}

		messages, err := app.MQ.GetMessages(app.Conf.Broker.Queue)
		if err != nil {
			log.Panic(err)
		}

		for delivered := range messages {
			log.Debugf("received a message (corr-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
			message := schema.IngestionTrigger{}
			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", app.Conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (ingestion-trigger) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed",
					Reason:          err.Error(),
					OriginalMessage: message,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := app.MQ.SendMessage(delivered.CorrelationId, app.Conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: (%s)", err.Error())
				}

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)
			log.Infof("Received work (corr-id: %s, filepath: %s, user: %s)", delivered.CorrelationId, message.FilePath, message.User)

			ackNack := ""
			switch message.Type {
			case "cancel":
				ackNack = app.cancelFile(delivered.CorrelationId, message)
			case "ingest":
				ackNack = app.ingestFile(delivered.CorrelationId, message)
			default:
				log.Errorln("unexpected ingest message type")
				if err := delivered.Reject(false); err != nil {
					log.Errorf("failed to reject message for reason: (%s)", err.Error())
				}
			}

			switch ackNack {
			case "ack":
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message for reason: (%s)", err.Error())
				}
			case "nack":
				if err = delivered.Nack(false, false); err != nil {
					log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
				}
			default:
				log.Errorln("unexpected ingest message type")
				if err := delivered.Reject(false); err != nil {
					log.Errorf("failed to reject message for reason: (%s)", err.Error())
				}
			}
		}
	}()

	<-forever
}

func (app *Ingest) cancelFile(correlationID string, message schema.IngestionTrigger) string {
	fileUUID, err := app.DB.GetFileID(correlationID)
	if err != nil {
		log.Errorf("failed to get ID for file from message: %s, reason: %s", correlationID, err.Error())
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return "reject"
		}

		return "nack"
	}

	m, _ := json.Marshal(message)
	if err := app.DB.UpdateFileEventLog(fileUUID, "disabled", correlationID, "ingest", "{}", string(m)); err != nil {
		log.Errorf("failed to set event log status for file: %s", correlationID)

		return "nack"
	}

	return "ack"
}

func (app *Ingest) ingestFile(correlationID string, message schema.IngestionTrigger) string {
	var fileID string
	status, err := app.DB.GetFileStatus(correlationID)
	if err != nil && err.Error() != "sql: no rows in result set" {
		log.Errorf("failed to get status for file, reason: (%s)", err.Error())

		return "nack"
	}

	switch status {
	case "disabled":
		fileID, err = app.DB.GetFileID(correlationID)
		if err != nil {
			log.Errorf("failed to get ID for file, reason: %s", err.Error())

			return "nack"
		}

		fileInfo, err := app.DB.GetFileInfo(fileID)
		if err != nil {
			log.Errorf("failed to get info for file: %s, reason: %s", fileID, err.Error())

			return "nack"
		}

		// What if the file in the inbox is different this time?
		// Check uploaded checksum in the DB against the checksum of the file.
		// Would be easy if the inbox message had the checksum or that the s3inbox added the checksum to the DB.

		if fileInfo.Checksum != "" {
			msg := schema.IngestionVerification{
				User:        message.User,
				FilePath:    message.FilePath,
				FileID:      fileID,
				ArchivePath: fileInfo.Path,
				EncryptedChecksums: []schema.Checksums{
					{Type: "sha256", Value: fileInfo.Checksum},
				},
			}
			archivedMsg, _ := json.Marshal(&msg)
			err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.Conf.Broker.SchemasPath), archivedMsg)
			if err != nil {
				log.Errorf("Validation of outgoing message failed, reason: (%s)", err.Error())

				return "nack"
			}

			m, _ := json.Marshal(message)
			if err = app.DB.UpdateFileEventLog(fileInfo.Path, "enabled", correlationID, "ingest", "{}", string(m)); err != nil {
				log.Errorf("failed to set ingestion status for file from message: %v", correlationID)

				return "nack"
			}

			if err := app.MQ.SendMessage(correlationID, app.Conf.Broker.Exchange, app.Conf.Broker.RoutingKey, archivedMsg); err != nil {
				log.Errorf("failed to publish message, reason: (%s)", err.Error())

				return "reject"
			}

			return "ack"
		}
	case "":
		// Catch all for inboxes that doesn't update the DB
		fileID, err = app.DB.RegisterFile(message.FilePath, message.User)
		if err != nil {
			log.Errorf("InsertFile failed, reason: (%s)", err.Error())

			return "nack"
		}
	case "uploaded":
		fileID, err = app.DB.GetFileID(correlationID)
		if err != nil {
			log.Errorf("failed to get ID for file, reason: %s", err.Error())

			return "nack"
		}
	default:
		return "reject"
	}

	file, err := app.Inbox.NewFileReader(message.FilePath)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "NoSuchKey:"):
			log.Errorf("Failed to open file to ingest reason: (%s)", err.Error())
			jsonMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			m, _ := json.Marshal(message)
			if err := app.DB.UpdateFileEventLog(fileID, "error", correlationID, "ingest", string(jsonMsg), string(m)); err != nil {
				log.Errorf("failed to set error status for file from message: %v, reason: %s", correlationID, err.Error())
			}
			// Send the message to an error queue so it can be analyzed.
			fileError := broker.InfoError{
				Error:           "Failed to open file to ingest",
				Reason:          err.Error(),
				OriginalMessage: message,
			}
			body, _ := json.Marshal(fileError)
			if err := app.MQ.SendMessage(correlationID, app.Conf.Broker.Exchange, "error", body); err != nil {
				log.Errorf("failed to publish message, reason: (%s)", err.Error())

				return "reject"
			}

			return "ack"
		default:
			return "nack"
		}
	}

	fileSize, err := app.Inbox.GetFileSize(message.FilePath, false)
	if err != nil {
		log.Errorf("Failed to get file size of file to ingest, reason: (%s)", err.Error())
		// Since reading the file worked, this should eventually succeed so it is ok to requeue.
		return "nack"
	}

	dest, err := app.Archive.NewFileWriter(fileID)
	if err != nil {
		log.Errorf("Failed to create archive file, reason: (%s)", err.Error())
		// NewFileWriter returns an error when the backend itself fails so this is reasonable to requeue.
		return "nack"
	}

	m, _ := json.Marshal(message)
	if err = app.DB.UpdateFileEventLog(fileID, "submitted", correlationID, "ingest", "{}", string(m)); err != nil {
		log.Errorf("failed to set ingestion status for file from message: %v", correlationID)
	}

	// 4MiB readbuffer, this must be large enough that we get the entire header and the first 64KiB datablock
	var bufSize int
	if bufSize = 4 * 1024 * 1024; app.Conf.Inbox.S3.Chunksize > 4*1024*1024 {
		bufSize = app.Conf.Inbox.S3.Chunksize
	}
	readBuffer := make([]byte, bufSize)
	hash := sha256.New()
	var bytesRead int64
	var byteBuf bytes.Buffer

	for bytesRead < fileSize {
		i, _ := io.ReadFull(file, readBuffer)
		if i == 0 {
			log.Debugln("this should not happen")

			return "reject"
		}
		// truncate the readbuffer if the file is smaller than the buffer size
		if i < len(readBuffer) {
			readBuffer = readBuffer[:i]
		}

		bytesRead += int64(i)

		h := bytes.NewReader(readBuffer)
		if _, err = io.Copy(hash, h); err != nil {
			log.Errorf("Copy to hash failed while reading file, reason: (%s)", err.Error())

			return "nack"
		}

		switch {
		case bytesRead <= int64(len(readBuffer)):
			var privateKey *[32]byte
			var header []byte

			// Iterate over the key list to try decryption
			for _, key := range app.ArchiveKeyList {
				header, err = tryDecrypt(key, readBuffer)
				if err == nil {
					privateKey = key

					break
				}
				log.Warnf("Decryption failed with key, trying next key. Reason: (%s)", err.Error())
			}

			// Check if decryption was successful with any key
			if privateKey == nil {
				log.Errorf("All keys failed to decrypt the submitted file")
				m, _ := json.Marshal(message)
				if err := app.DB.UpdateFileEventLog(fileID, "error", correlationID, "ingest", `{"error" : "Decryption failed with all available key(s)"}`, string(m)); err != nil {
					log.Errorf("Failed to set ingestion status for file from message: %v", correlationID)
				}

				// Send the message to an error queue so it can be analyzed.
				fileError := broker.InfoError{
					Error:           "Trying to decrypt the submitted file failed",
					Reason:          "Decryption failed with the available key(s)",
					OriginalMessage: message,
				}
				body, _ := json.Marshal(fileError)
				if err := app.MQ.SendMessage(correlationID, app.Conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}

				return "ack"
			}

			// Proceed with the successful key
			// Set the file's hex encoded public key
			publicKey := keys.DerivePublicKey(*privateKey)
			keyhash := hex.EncodeToString(publicKey[:])
			err = app.DB.SetKeyHash(keyhash, fileID)
			if err != nil {
				log.Errorf("Key hash %s could not be set for fileID %s: (%s)", keyhash, fileID, err.Error())

				return "nack"
			}

			log.Debugln("store header")
			if err := app.DB.StoreHeader(header, fileID); err != nil {
				log.Errorf("StoreHeader failed, reason: (%s)", err.Error())

				return "nack"
			}

			if _, err = byteBuf.Write(readBuffer); err != nil {
				log.Errorf("Failed to write to read buffer for header read, reason: %v)", err.Error())

				return "nack"
			}

			// Strip header from buffer
			h := make([]byte, len(header))
			if _, err = byteBuf.Read(h); err != nil {
				log.Errorf("Failed to strip header from buffer, reason: (%s)", err.Error())

				return "nack"
			}
		default:
			if i < len(readBuffer) {
				readBuffer = readBuffer[:i]
			}
			if _, err = byteBuf.Write(readBuffer); err != nil {
				log.Errorf("Failed to write to read buffer for full read, reason: (%s)", err.Error())

				return "nack"
			}
		}

		// Write data to file
		if _, err = byteBuf.WriteTo(dest); err != nil {
			log.Errorf("Failed to write to archive file, reason: (%s)", err.Error())

			_ = file.Close()
			_ = dest.Close()
			if err := app.Archive.RemoveFile(fileID); err != nil {
				log.Errorf("error when removing uncompleted file: %s", fileID)
			}

			return "nack"
		}
	}

	file.Close()
	dest.Close()

	// At this point we should do checksum comparison, but that requires updating the AWS library

	fileInfo := database.FileInfo{}
	fileInfo.Path = fileID
	fileInfo.UploadedChecksum = fmt.Sprintf("%x", hash.Sum(nil))
	fileInfo.Size, err = app.Archive.GetFileSize(fileID, true)
	if err != nil {
		log.Errorf("Couldn't get file size from archive, reason: %v)", err.Error())

		return "nack"
	}

	log.Debugf("Wrote archived file (corr-id: %s, user: %s, filepath: %s, archivepath: %s, archivedsize: %d)", correlationID, message.User, message.FilePath, fileID, fileInfo.Size)

	status, err = app.DB.GetFileStatus(correlationID)
	if err != nil {
		log.Errorf("failed to get file status, reason: (%s)", err.Error())

		return "nack"
	}

	if status == "disabled" {
		log.Infof("file with correlation ID: %s is disabled, stopping ingestion", correlationID)

		return "ack"
	}

	if err := app.DB.SetArchived(fileInfo, fileID, correlationID); err != nil {
		log.Errorf("SetArchived failed, reason: (%s)", err.Error())

		return "nack"
	}

	if err := app.DB.UpdateFileEventLog(fileID, "archived", correlationID, "ingest", "{}", string(m)); err != nil {
		log.Errorf("failed to set event log status for file: %s", correlationID)

		return "nack"
	}
	log.Debugf("File marked as archived (corr-id: %s, user: %s, filepath: %s, archivepath: %s)", correlationID, message.User, message.FilePath, fileID)

	// Send message to archived
	msg := schema.IngestionVerification{
		User:        message.User,
		FilePath:    message.FilePath,
		FileID:      fileID,
		ArchivePath: fileID,
		EncryptedChecksums: []schema.Checksums{
			{Type: "sha256", Value: fmt.Sprintf("%x", hash.Sum(nil))},
		},
	}
	archivedMsg, _ := json.Marshal(&msg)

	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.Conf.Broker.SchemasPath), archivedMsg)
	if err != nil {
		log.Errorf("Validation of outgoing message failed, reason: (%s)", err.Error())

		return "nack"
	}

	if err := app.MQ.SendMessage(correlationID, app.Conf.Broker.Exchange, app.Conf.Broker.RoutingKey, archivedMsg); err != nil {
		// TODO fix resend mechanism
		log.Errorf("failed to publish message, reason: (%s)", err.Error())

		return "reject"
	}

	return "ack"
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
