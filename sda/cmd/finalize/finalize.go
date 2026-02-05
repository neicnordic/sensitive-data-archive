// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

var db *database.SDAdb
var mqBroker *broker.AMQPBroker
var archiveReader storage.Reader
var backupWriter storage.Writer
var message schema.IngestionAccession

var backupInStorage bool

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conf, err := config.NewConfig("finalize")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer db.Close()

	if db.Version < 23 {
		return errors.New("database schema v23 is required")
	}

	mqBroker, err = broker.NewMQ(conf.Broker)
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer func() {
		if err := mqBroker.Channel.Close(); err != nil {
			log.Errorf("failed to close mq broker channel due to: %v", err)
		}
		if err := mqBroker.Connection.Close(); err != nil {
			log.Errorf("failed to close mq broker connection due to: %v", err)
		}
	}()

	lb, err := locationbroker.NewLocationBroker(db)
	if err != nil {
		return fmt.Errorf("failed to init new location broker, due to: %v", err)
	}
	backupWriter, err = storage.NewWriter(ctx, "backup", lb)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidWriter) {
		return fmt.Errorf("failed to initialize backup writer, due to: %v", err)
	}
	archiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidReader) {
		return fmt.Errorf("failed to initialize archive reader: %v", err)
	}

	if archiveReader != nil && backupWriter != nil {
		backupInStorage = true
	} else {
		log.Warn("archive or backup destination not configured, backup will not be performed.")
	}

	log.Info("Starting finalize service")
	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- startConsumer(ctx)
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
	case err := <-mqBroker.Connection.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-mqBroker.Channel.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-consumeErr:
		return err
	}

	return nil
}
func startConsumer(ctx context.Context) error {
	messages, err := mqBroker.GetMessages(mqBroker.Conf.Queue)
	if err != nil {
		return err
	}
	for delivered := range messages {
		handleMessage(ctx, delivered)
	}

	return nil
}

func handleMessage(ctx context.Context, delivered amqp.Delivery) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	log.Debugf("Received a message (correlation-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", mqBroker.Conf.SchemasPath), delivered.Body); err != nil {
		log.Errorf("validation of incoming message (ingestion-accession) failed, correlation-id: %s, reason: %v ", delivered.CorrelationId, err)
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: %v", err)
		}

		return
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

		return
	}

	switch status {
	case "disabled":
		log.Infof("file with file-id: %s is disabled, aborting work", fileID)
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: %v", err)
		}

		return
	case "verified", "enabled":
	case "ready":
		log.Infof("File with file-id: %s is already marked as ready.", fileID)
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking message, reason: %v", err)
		}

		return
	default:
		log.Warnf("file with file-id: %s is not verified yet, aborting work", fileID)
		if err := delivered.Nack(false, true); err != nil {
			log.Errorf("Failed acking canceled work, reason: %v", err)
		}

		return
	}

	c := schema.IngestionCompletion{
		User:               message.User,
		FilePath:           message.FilePath,
		AccessionID:        message.AccessionID,
		DecryptedChecksums: message.DecryptedChecksums,
	}
	completeMsg, _ := json.Marshal(&c)

	if err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", mqBroker.Conf.SchemasPath), completeMsg); err != nil {
		log.Errorf("Validation of outgoing message ingestion-completion failed, reason: (%v). Message body: %s\n", err, string(completeMsg))

		return
	}

	accessionIDExists, err := db.CheckAccessionIDExists(message.AccessionID, fileID)
	if err != nil {
		log.Errorf("CheckAccessionIdExists failed, file-id: %s, reason: %v ", fileID, err)
		if err := delivered.Nack(false, true); err != nil {
			log.Errorf("failed to Nack message, reason: %v", err)
		}

		return
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
		if e := mqBroker.SendMessage(fileID, mqBroker.Conf.Exchange, "error", body); e != nil {
			log.Errorf("failed to publish message, reason: %v", err)
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: %v", err)
		}

		return
	case "same":
		log.Infof("file already has a stable ID, marking it as ready, file-id: %s", fileID)
	default:
		if backupInStorage {
			if err = backupFile(ctx, delivered); err != nil {
				log.Errorf("failed to backup file, file-id: %s, reason: %v", fileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}

				return
			}
		}

		if err := db.SetAccessionID(message.AccessionID, fileID); err != nil {
			log.Errorf("failed to set accessionID for file, file-id: %s, reason: %v", fileID, err)
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Nack message, reason: %v", err)
			}

			return
		}
	}

	// Mark file as "ready"
	if err := db.UpdateFileEventLog(fileID, "ready", "finalize", "{}", string(delivered.Body)); err != nil {
		log.Errorf("set status ready failed, file-id: %s, reason: %v", fileID, err)
		if err := delivered.Nack(false, true); err != nil {
			log.Errorf("failed to Nack message, reason: %v", err)
		}

		return
	}

	if err := mqBroker.SendMessage(fileID, mqBroker.Conf.Exchange, mqBroker.Conf.RoutingKey, completeMsg); err != nil {
		log.Errorf("failed to publish message, reason: %v", err)
		if err := delivered.Nack(false, true); err != nil {
			log.Errorf("failed to Nack message, reason: %v", err)
		}

		return
	}

	if err := delivered.Ack(false); err != nil {
		log.Errorf("failed to Ack message, reason: %v", err)
	}
}

func backupFile(ctx context.Context, delivered amqp.Delivery) error {
	log.Debug("Backup initiated")
	fileID := delivered.CorrelationId

	archiveData, err := db.GetArchived(fileID)
	if err != nil {
		return fmt.Errorf("failed to get file archive information, reason: %v", err)
	}

	// Get size on disk, will also give some time for the file to appear if it has not already
	diskFileSize, err := archiveReader.GetFileSize(ctx, archiveData.Location, archiveData.FilePath)
	if err != nil {
		return fmt.Errorf("failed to get size info for archived file, reason: %v", err)
	}

	if diskFileSize != int64(archiveData.FileSize) {
		return fmt.Errorf("archive file size does not match registered file size, (disk size: %d, db size: %d)", diskFileSize, archiveData.FileSize)
	}

	file, err := archiveReader.NewFileReader(ctx, archiveData.Location, archiveData.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open archived file, reason: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()

	contentReader, contentWriter := io.Pipe()
	go func() {
		defer func() {
			_ = contentWriter.Close()
		}()

		if copiedSize, err := io.Copy(contentWriter, file); err != nil {
			_ = contentWriter.CloseWithError(fmt.Errorf("failed to copy file, reason: %v)", err))
		} else if copiedSize != int64(archiveData.FileSize) {
			_ = contentWriter.CloseWithError(errors.New("copied size does not match file size"))
		}
	}()

	_, err = backupWriter.WriteFile(ctx, archiveData.FilePath, contentReader)
	if err != nil {
		return fmt.Errorf("failed to write file to backup storage, reason: %v", err)
	}
	_ = contentReader.Close()

	// Mark file as "backed up"
	if err := db.UpdateFileEventLog(fileID, "backed up", "finalize", "{}", string(delivered.Body)); err != nil {
		return fmt.Errorf("UpdateFileEventLog failed, reason: (%v)", err)
	}

	log.Debug("Backup completed")

	return nil
}
