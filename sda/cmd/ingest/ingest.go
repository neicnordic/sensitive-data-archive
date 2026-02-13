// The ingest service accepts messages for files uploaded to the inbox,
// registers the files in the database with their headers, and stores them
// header-stripped in the archive storage.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type Ingest struct {
	ArchiveWriter  storage.Writer
	BackupWriter   storage.Writer
	ArchiveReader  storage.Reader
	ArchiveKeyList []*[32]byte
	DB             *database.SDAdb
	InboxReader    storage.Reader
	MQ             *broker.AMQPBroker
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := Ingest{}
	ingestConf, err := config.NewConfig("ingest")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}
	app.MQ, err = broker.NewMQ(ingestConf.Broker)
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer func() {
		if app.MQ == nil {
			return
		}
		if app.MQ.Channel != nil {
			if err := app.MQ.Channel.Close(); err != nil {
				log.Errorf("failed to close mq broker channel due to: %v", err)
			}
		}
		if app.MQ.Connection != nil {
			if err := app.MQ.Connection.Close(); err != nil {
				log.Errorf("failed to close mq broker connection due to: %v", err)
			}
		}
	}()
	app.DB, err = database.NewSDAdb(ingestConf.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db due to: %v", err)
	}
	defer app.DB.Close()
	if app.DB.Version < 23 {
		return errors.New("database schema v23 is required")
	}
	app.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil || len(app.ArchiveKeyList) == 0 {
		return errors.New("no C4GH private keys configured")
	}

	if err := app.registerC4GHKey(); err != nil {
		return fmt.Errorf("failed to register c4gh key, due to: %v", err)
	}

	storageLocationBroker, err := locationbroker.NewLocationBroker(app.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker, due to: %v", err)
	}
	app.ArchiveWriter, err = storage.NewWriter(ctx, "archive", storageLocationBroker)
	if err != nil {
		return fmt.Errorf("failed to initialize archive writer, due to: %v", err)
	}
	app.ArchiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil {
		return fmt.Errorf("failed to initialize archive reader, due to: %v", err)
	}
	app.InboxReader, err = storage.NewReader(ctx, "inbox")
	if err != nil {
		return fmt.Errorf("failed to initialize inbox reader, due to: %v", err)
	}

	backupWriter, err := storage.NewWriter(ctx, "backup", storageLocationBroker)
	if err != nil && errors.Is(err, storageerrors.ErrorNoValidWriter) {
		return fmt.Errorf("failed to initialize backup writer due to: %v", err)
	}
	if backupWriter != nil {
		log.Info("backup writer initialized, will clean cancelled files from backup storage")
		app.BackupWriter = backupWriter
	} else {
		log.Info("no backup writer initialized, will NOT clean cancelled files from backup storage")
	}
	log.Info("starting ingest service")

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.startConsumer(ctx)
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
	case err := <-app.MQ.Connection.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-app.MQ.Channel.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-consumeErr:
		return err
	}

	return nil
}

func (app *Ingest) startConsumer(ctx context.Context) error {
	messages, err := app.MQ.GetMessages(app.MQ.Conf.Queue)
	if err != nil {
		return err
	}

	for delivered := range messages {
		app.handleMessage(ctx, delivered)
	}

	return nil
}

func (app *Ingest) handleMessage(ctx context.Context, delivered amqp.Delivery) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	log.Debugf("received a message (correlation-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)

	err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", app.MQ.Conf.SchemasPath), delivered.Body)
	if err != nil {
		log.Errorf("validation of incoming message (ingestion-trigger) failed, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
		// Send the message to an error queue so it can be analyzed.
		infoErrorMessage := broker.InfoError{
			Error:           "Message validation failed",
			Reason:          err.Error(),
			OriginalMessage: delivered,
		}

		body, _ := json.Marshal(infoErrorMessage)
		if err := app.MQ.SendMessage(delivered.CorrelationId, app.MQ.Conf.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: %v", err)
		}
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: %v", err)
		}

		return
	}
	message := schema.IngestionTrigger{}
	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(delivered.Body, &message)
	log.Infof("Received work (correlation-id: %s, filepath: %s, user: %s)", delivered.CorrelationId, message.FilePath, message.User)

	ackNack := ""
	switch message.Type {
	case "cancel":
		ackNack = app.cancelFile(ctx, delivered.CorrelationId, message)
	case "ingest":
		ackNack = app.ingestFile(ctx, delivered.CorrelationId, message)
	default:
		log.Errorln("unexpected ingest message type")
		if err := delivered.Reject(false); err != nil {
			log.Errorf("failed to reject message, reason: %v", err)
		}
	}

	switch ackNack {
	case "ack":
		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to ack message, reason: %v", err)
		}
	case "nack":
		if err = delivered.Nack(false, false); err != nil {
			log.Errorf("failed to Nack message, reason: %v", err)
		}
	default:
		// will catch `reject`s, failures that should not be requeued.
		if err := delivered.Reject(false); err != nil {
			log.Errorf("failed to reject message, reason: %v", err)
		}
	}
}

func (app *Ingest) registerC4GHKey() error {
	h, err := app.DB.ListKeyHashes()
	if err != nil {
		return err
	}
	if len(h) == 0 {
		for num, key := range app.ArchiveKeyList {
			publicKey := keys.DerivePublicKey(*key)
			if err := app.DB.AddKeyHash(hex.EncodeToString(publicKey[:]), fmt.Sprintf("bootstrapped key: %d", num)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (app *Ingest) cancelFile(ctx context.Context, fileID string, message schema.IngestionTrigger) string {
	m, _ := json.Marshal(message)

	// Check if file can be cancelled
	inDataset, err := app.DB.IsFileInDataset(ctx, fileID)
	if err != nil {
		log.Errorf("failed to check if file with id: %s is in a dataset, due to %v", fileID, err)

		return "nack"
	}
	if inDataset {
		log.Warnf("can not cancel file with id: %s, as it has been added to a dataset", fileID)

		fileError := broker.InfoError{
			Error:           "Cancel of file not possible",
			Reason:          "File has been added to a dataset",
			OriginalMessage: message,
		}
		body, _ := json.Marshal(fileError)
		if err := app.MQ.SendMessage(fileID, app.MQ.Conf.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: %v", err)

			return "reject"
		}

		return "ack"
	}

	archiveData, err := app.DB.GetArchived(fileID)
	if err != nil {
		log.Errorf("failed to get archive data for file with id: %s, due to %v", fileID, err)

		return "nack"
	}

	if archiveData == nil {
		log.Warnf("file with id: %s, could not be cancelled, as it has not yet been archived", fileID)

		return "reject"
	}

	if archiveData.Location != "" {
		if err := app.ArchiveWriter.RemoveFile(ctx, archiveData.Location, archiveData.FilePath); err != nil {
			log.Errorf("failed to remove file with id %s from archive due to %v", fileID, err)

			return "nack"
		}
	}

	if app.BackupWriter != nil && archiveData.BackupFilePath != "" && archiveData.BackupLocation != "" {
		if err := app.BackupWriter.RemoveFile(ctx, archiveData.BackupLocation, archiveData.BackupFilePath); err != nil {
			log.Errorf("failed to remove file with id %s from backup due to %v", fileID, err)

			return "nack"
		}
	}

	if err := app.DB.CancelFile(ctx, fileID, string(m)); err != nil {
		log.Errorf("failed to cancel file with id: %s, due to %v", fileID, err)

		return "nack"
	}

	return "ack"
}

func (app *Ingest) ingestFile(ctx context.Context, fileID string, message schema.IngestionTrigger) string {
	status, err := app.DB.GetFileStatus(fileID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Errorf("failed to get status for file, fileID: %s, reason: (%s)", fileID, err.Error())

		return "nack"
	}

	submissionLocation, err := app.DB.GetSubmissionLocation(ctx, fileID)
	if err != nil {
		log.Errorf("failed to get submission location for file, fileID: %s, reason: (%s)", fileID, err.Error())

		return "nack"
	}

	if status != "" && submissionLocation == "" {
		log.Errorf("file %s has been registered but has no submission location", fileID)

		return "nack"
	}

	switch status {
	case "":
		// Catch all for implementations that don't update the DB, e.g. for those not using S3inbox or sftpInbox
		// Since we dont have the submission location in storage, we need to look through all configured storage locations.
		var findFileErr, registerErr error
		submissionLocation, findFileErr = app.InboxReader.FindFile(ctx, message.FilePath)
		// Register file even if FindFile didnt succeed with submissionLocation == "", as we will add an error file event log to it in that case
		fileID, registerErr = app.DB.RegisterFile(&fileID, submissionLocation, message.FilePath, message.User)
		if registerErr != nil {
			log.Errorf("failed to register file, fileID: %s, reason: (%s)", fileID, err.Error())

			return "nack"
		}

		if findFileErr != nil {
			log.Errorf("failed to find submission location for file in all configured storage locations, file-id: %s", fileID)
			if err := app.setFileEventErrorAndSendToErrorQueue(fileID, &broker.InfoError{
				Error:           "Failed to open file to ingest, file not found in any of the configured storage locations",
				Reason:          findFileErr.Error(),
				OriginalMessage: message,
			}); err != nil {
				return "reject"
			}

			return "ack"
		}

	case "uploaded", "disabled":

	default:
		log.Warnf("unsupported file status: %s, file-id: %s", status, fileID)

		return "reject"
	}

	file, err := app.InboxReader.NewFileReader(ctx, submissionLocation, helper.UnanonymizeFilepath(message.FilePath, message.User))
	if err != nil {
		if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
			log.Errorf("Failed to open file to ingest reason: (%s)", err.Error())
			if err := app.setFileEventErrorAndSendToErrorQueue(fileID, &broker.InfoError{
				Error:           "Failed to open file to ingest",
				Reason:          err.Error(),
				OriginalMessage: message,
			}); err != nil {
				return "reject"
			}

			return "ack"
		}
		log.Errorf("unexpected error when opening file for reading, file-id: %s, filepath: %s, reason: %s", fileID, message.FilePath, err.Error())

		return "nack"
	}
	// Ensure file is closed in case we encounter error, etc
	defer func() {
		_ = file.Close()
	}()

	fileSize, err := app.InboxReader.GetFileSize(ctx, submissionLocation, helper.UnanonymizeFilepath(message.FilePath, message.User))
	if err != nil {
		log.Errorf("Failed to get file size of file to ingest, file-id: %s, filepath: %s, reason: (%s)", fileID, message.FilePath, err.Error())
		// Since reading the file worked, this should eventually succeed so it is ok to requeue.
		return "nack"
	}

	m, _ := json.Marshal(message)
	if err = app.DB.UpdateFileEventLog(fileID, "submitted", "ingest", "{}", string(m)); err != nil {
		log.Errorf("failed to set ingestion status for file from message, file-id: %s, reason: %s", fileID, err.Error())
	}

	// 50MiB readbuffer, this must be large enough that we get the entire header and the first 64KiB datablock
	bufSize := 50 * 1024 * 1024
	readBuffer := make([]byte, bufSize)
	hash := sha256.New()
	var bytesRead int64
	var byteBuf bytes.Buffer
	contentReader, contentWriter := io.Pipe()
	// Ensure these are closed in case we encounter error
	defer func() {
		_ = contentReader.Close()
		_ = contentWriter.Close()
	}()

	uploadCtx, uploadCancel := context.WithCancel(ctx)
	defer uploadCancel()
	readFileAck := make(chan string, 1)

	go func() {
		for bytesRead < fileSize {
			// If storageWriter has encountered an error, and we've exited, we want to stop this goroutine as well
			if uploadCtx.Err() != nil {
				return
			}
			i, _ := io.ReadFull(file, readBuffer)
			if i == 0 {
				log.Errorf("readBuffer returned 0 bytes, this should not happen, file-id: %s", fileID)
				readFileAck <- "reject"
				uploadCancel()

				return
			}
			// truncate the readbuffer if the file is smaller than the buffer size
			if i < len(readBuffer) {
				readBuffer = readBuffer[:i]
			}

			bytesRead += int64(i)

			h := bytes.NewReader(readBuffer)
			if _, err = io.Copy(hash, h); err != nil {
				log.Errorf("Copy to hash failed while reading file, file-id: %s, reason: (%s)", fileID, err.Error())
				readFileAck <- "nack"
				uploadCancel()

				return
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
					log.Warnf("Decryption failed with key, trying next key. file-id: %s, reason: (%s)", fileID, err.Error())
				}

				// Check if decryption was successful with any key
				if privateKey == nil {
					log.Errorf("All keys failed to decrypt the submitted file, file-id: %s", fileID)
					if err := app.setFileEventErrorAndSendToErrorQueue(fileID, &broker.InfoError{
						Error:           "Trying to decrypt the submitted file failed",
						Reason:          "Decryption failed with the available key(s)",
						OriginalMessage: message,
					}); err != nil {
						readFileAck <- "reject"
						uploadCancel()

						return
					}
					readFileAck <- "ack"
					uploadCancel()

					return
				}

				// Proceed with the successful key
				// Set the file's hex encoded public key
				publicKey := keys.DerivePublicKey(*privateKey)
				keyhash := hex.EncodeToString(publicKey[:])
				err = app.DB.SetKeyHash(keyhash, fileID)
				if err != nil {
					log.Errorf("Key hash %s could not be set for file, file-id: %s, reason: (%s)", keyhash, fileID, err.Error())
					readFileAck <- "nack"
					uploadCancel()

					return
				}

				log.Debugln("store header")
				if err := app.DB.StoreHeader(header, fileID); err != nil {
					log.Errorf("StoreHeader failed, file-id: %s, reason: (%s)", fileID, err.Error())
					readFileAck <- "nack"
					uploadCancel()

					return
				}

				if _, err = byteBuf.Write(readBuffer); err != nil {
					log.Errorf("Failed to write to read buffer for header read, file-id: %s, reason: %v)", fileID, err.Error())
					readFileAck <- "nack"
					uploadCancel()

					return
				}

				// Strip header from buffer
				h := make([]byte, len(header))
				if _, err = byteBuf.Read(h); err != nil {
					log.Errorf("Failed to strip header from buffer, file-id: %s, reason: (%s)", fileID, err.Error())
					readFileAck <- "nack"
					uploadCancel()

					return
				}
			default:
				if i < len(readBuffer) {
					readBuffer = readBuffer[:i]
				}
				if _, err = byteBuf.Write(readBuffer); err != nil {
					log.Errorf("Failed to write to read buffer for full read, file-id: %s, reason: (%s)", fileID, err.Error())
					readFileAck <- "nack"
					uploadCancel()

					return
				}
			}

			// Write data to file
			if _, err = byteBuf.WriteTo(contentWriter); err != nil {
				log.Errorf("Failed to write to archive file, file-id: %s, reason: (%s)", fileID, err.Error())
				readFileAck <- "nack"
				uploadCancel()

				return
			}
		}

		_ = contentWriter.Close()
		_ = file.Close()
	}()

	uploadErr := make(chan error, 1)
	var location string
	go func() {
		var err error
		location, err = app.ArchiveWriter.WriteFile(uploadCtx, fileID, contentReader)
		uploadErr <- err
	}()

	// React to first issue, either from storage writer of file reader
	select {
	case ack := <-readFileAck:
		// if ack != "" the reading of data has encountered an error and we should ack this message with the code
		if ack != "" {
			return ack
		}
	case err := <-uploadErr:
		if err != nil {
			log.Errorf("Failed to upload archive file, file-id: %s, reason: (%s)", fileID, err.Error())

			return "nack"
		}
	}
	// As we are done with uploadCtx now, we cancel it
	uploadCancel()
	_ = contentReader.Close()

	// At this point we should do checksum comparison, but that requires updating the AWS library
	fileInfo := database.FileInfo{}
	fileInfo.Path = fileID
	fileInfo.UploadedChecksum = fmt.Sprintf("%x", hash.Sum(nil))
	fileInfo.Size, err = app.ArchiveReader.GetFileSize(ctx, location, fileID)
	if err != nil {
		log.Errorf("Couldn't get file size from archive, file-id: %s, reason: %v)", fileID, err.Error())

		return "nack"
	}

	log.Debugf("Wrote archived file (file-id: %s, user: %s, filepath: %s, archivepath: %s, archivedsize: %d)", fileID, message.User, message.FilePath, fileID, fileInfo.Size)

	status, err = app.DB.GetFileStatus(fileID)
	if err != nil {
		log.Errorf("failed to get file status, file-id: %s, reason: (%s)", fileID, err.Error())

		return "nack"
	}

	if status == "disabled" {
		log.Infof("file is disabled, stopping ingestion, file-id: %s", fileID)

		return "ack"
	}

	if err := app.DB.SetArchived(location, fileInfo, fileID); err != nil {
		log.Errorf("SetArchived failed, file-id: %s, reason: (%s)", fileID, err.Error())

		return "nack"
	}

	if err := app.DB.UpdateFileEventLog(fileID, "archived", "ingest", "{}", string(m)); err != nil {
		log.Errorf("failed to set event log status for file, file-id: %s, reason: %s", fileID, err.Error())

		return "nack"
	}
	log.Debugf("File marked as archived (file-id: %s, user: %s, filepath: %s)", fileID, message.User, message.FilePath)

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

	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.MQ.Conf.SchemasPath), archivedMsg)
	if err != nil {
		log.Errorf("Validation of outgoing message failed, file-id: %s, reason: (%s)", fileID, err.Error())

		return "nack"
	}

	if err := app.MQ.SendMessage(fileID, app.MQ.Conf.Exchange, app.MQ.Conf.RoutingKey, archivedMsg); err != nil {
		// TODO fix resend mechanism
		log.Errorf("failed to publish message, reason: %v", err)

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

func (app *Ingest) setFileEventErrorAndSendToErrorQueue(fileID string, infoError *broker.InfoError) error {
	jsonMsg, _ := json.Marshal(map[string]string{"error": infoError.Error, "reason": infoError.Reason})
	m, _ := json.Marshal(infoError.OriginalMessage)
	if err := app.DB.UpdateFileEventLog(fileID, "error", "ingest", string(jsonMsg), string(m)); err != nil {
		log.Errorf("failed to set error status for file from message, file-id: %s, reason: %s", fileID, err.Error())
	}
	body, _ := json.Marshal(infoError)
	if err := app.MQ.SendMessage(fileID, app.MQ.Conf.Exchange, "error", body); err != nil {
		log.Errorf("failed to publish message, reason: %v", err)

		return err
	}

	return nil
}
