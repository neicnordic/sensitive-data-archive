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

	appconf "github.com/neicnordic/sensitive-data-archive/cmd/finalize/config"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"

	log "github.com/sirupsen/logrus"
)

type Finalize struct {
	archiveReader storage.Reader
	backupWriter  storage.Writer
	broker        brokerv2.Broker
	db            database.Database
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	var err error
	app := Finalize{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	app.db, err = postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer app.db.Close()

	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	app.broker, err = rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer func() {
		if app.broker == nil {
			return
		}
		if err := app.broker.Close(); err != nil {
			log.Errorf("could not close broker, reason: %v", err)
		}
	}()

	lb, err := locationbroker.NewLocationBroker(app.db)
	if err != nil {
		return fmt.Errorf("failed to init new location broker, due to: %v", err)
	}
	app.backupWriter, err = storage.NewWriter(ctx, "backup", lb)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidWriter) {
		return fmt.Errorf("failed to initialize backup writer, due to: %v", err)
	}
	app.archiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidReader) {
		return fmt.Errorf("failed to initialize archive reader: %v", err)
	}

	if app.archiveReader == nil || app.backupWriter == nil {
		log.Warn("archive or backup destination not configured, backup will not be performed.")
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, appconf.SourceQueue(), app.handleMessage)
	}()
	log.Info("Starting finalize service")

	select {
	case sig := <-sigc:
		log.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			log.Errorf("consumer failure, reason: %v", err)
			cancel()

			return err
		}

		return nil
	}
}

func (app *Finalize) handleMessage(ctx context.Context, message *brokerv2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Debugf("Received a message (correlation-id: %s, message: %s)", message.Key, message.Body)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", appconf.SchemaPath()), message.Body); err != nil {
		log.Errorf("validation of incoming message (ingestion-accession) failed, correlation-id: %s, reason: %v ", message.Key, err)

		return []func(){app.errorQueue(message, "could not validate message")}, nil
	}

	var ingestionAccession schema.IngestionAccession
	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(message.Body, &ingestionAccession)
	// If the file has been canceled by the uploader, don't spend time working on it.
	status, err := app.db.GetFileStatus(ctx, message.Key)
	if err != nil {
		log.Errorf("failed to get file status, file-id: %s, reason: %v", message.Key, err)

		return nil, err
	}

	var callbacks []func()
	switch status {
	case "":
		return []func(){app.errorQueue(message, "file not recognized")}, nil
	case "disabled":
		log.Debugf("file with file-id: %s is disabled, aborting work", message.Key)

		return nil, nil
	case "verified", "enabled", "backed up":
		callbacks, err = app.setAccession(ctx, &ingestionAccession, message)
	case "ready":
		log.Debugf("File with file-id: %s is already marked as ready.", message.Key)

		return nil, nil
	default:
		log.Warnf("file with file-id: %s is not verified yet, aborting work", message.Key)

		return nil, fmt.Errorf("file with file-id: %s is not verified yet, aborting work", message.Key)
	}

	return callbacks, err
}

func (app *Finalize) backupFile(ctx context.Context, tx database.Transaction, message *brokerv2.Message) ([]func(), error) {
	log.Debug("Backup initiated")

	archiveData, err := app.db.GetArchived(ctx, message.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get file archive information, reason: %v", err)
	}

	if archiveData == nil {
		return nil, fmt.Errorf("file archive data not found in database, file-id: %s", message.Key)
	}

	// Get size on disk, will also give some time for the file to appear if it has not already
	diskFileSize, err := app.archiveReader.GetFileSize(ctx, archiveData.Location, archiveData.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get size info for archived file, reason: %v", err)
	}

	if diskFileSize != archiveData.FileSize {
		return []func(){app.errorQueue(message, "archive file size does not match registered file size")}, fmt.Errorf("archive file size does not match registered file size, (disk size: %d, db size: %d)", diskFileSize, archiveData.FileSize)
	}

	file, err := app.archiveReader.NewFileReader(ctx, archiveData.Location, archiveData.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archived file, reason: %v", err)
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
			_ = contentWriter.CloseWithError(fmt.Errorf("failed to copy file, reason: %v", err))
		} else if copiedSize != archiveData.FileSize {
			_ = contentWriter.CloseWithError(errors.New("copied size does not match file size"))
		}
	}()

	backupLocation, err := app.backupWriter.WriteFile(ctx, archiveData.FilePath, contentReader)
	if err != nil {
		_ = contentReader.Close()

		return nil, fmt.Errorf("failed to write file to backup storage, reason: %v", err)
	}
	_ = contentReader.Close()

	// Mark file as "backed up" and populate backup path and location
	if err := tx.SetBackedUp(ctx, backupLocation, archiveData.FilePath, message.Key); err != nil {
		return nil, fmt.Errorf("SetBackedUp failed, reason: (%v)", err)
	}

	if err := tx.UpdateFileEventLog(ctx, message.Key, "backed up", "finalize", "{}", string(message.Body)); err != nil {
		return nil, fmt.Errorf("UpdateFileEventLog failed, reason: (%v)", err)
	}

	log.Debug("Backup completed")

	return nil, nil
}

func (app *Finalize) setAccession(ctx context.Context, ingestionAccession *schema.IngestionAccession, message *brokerv2.Message) ([]func(), error) {
	accessionIDExists, err := app.db.CheckAccessionIDExists(ctx, ingestionAccession.AccessionID, message.Key)
	if err != nil {
		log.Errorf("CheckAccessionIdExists failed, file-id: %s, reason: %v ", message.Key, err)

		return nil, err
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		log.Errorf("failed to begin transaction, reason: %v", err)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Errorf("failed to rollback transaction, reason: %v", err)
		}
	}()

	switch accessionIDExists {
	case "duplicate":
		log.Errorf("accession ID already exists in the system, file-id: %s, accession-id: %s\n", message.Key, ingestionAccession.AccessionID)
		// Send the message to an error queue so it can be analyzed.
		return []func(){app.errorQueue(message, "Duplicate accession ID")}, nil
	case "same":
		log.Infof("file already has an accession ID, marking it as ready, file-id: %s", message.Key)
	default:
		if app.archiveReader != nil && app.backupWriter != nil {
			if callbacks, err := app.backupFile(ctx, tx, message); err != nil {
				log.Errorf("failed to backup file, file-id: %s, reason: %v", message.Key, err)

				if callbacks != nil {
					// Send the message to an error queue  but don't requeue it
					return callbacks, nil
				}

				return nil, err
			}
		}

		if err := tx.SetAccessionID(ctx, ingestionAccession.AccessionID, message.Key); err != nil {
			log.Errorf("failed to set accessionID for file, file-id: %s, reason: %v", message.Key, err)

			return nil, err
		}
	}

	if err := tx.UpdateFileEventLog(ctx, message.Key, "ready", "finalize", "{}", string(message.Body)); err != nil {
		log.Errorf("set status ready failed, file-id: %s, reason: %v", message.Key, err)

		return nil, err
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit transaction, reason: %v", err)
		// requeue message as broker error is not expected and should succeed on retries
		return nil, err
	}

	if err := app.sendCompleted(ctx, message.Key, ingestionAccession); err != nil {
		return nil, err
	}

	return nil, nil
}

func (app *Finalize) sendCompleted(ctx context.Context, fileID string, ingestionAccession *schema.IngestionAccession) error {
	c := schema.IngestionCompletion{
		User:               ingestionAccession.User,
		FilePath:           ingestionAccession.FilePath,
		AccessionID:        ingestionAccession.AccessionID,
		DecryptedChecksums: ingestionAccession.DecryptedChecksums,
	}
	completeMsg, _ := json.Marshal(&c)

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", appconf.SchemaPath()), completeMsg); err != nil {
		return err
	}

	completedMessage := brokerv2.Message{
		Key:  fileID,
		Body: completeMsg,
	}

	return app.broker.Publish(ctx, appconf.RoutingKey(), completedMessage)
}

func (app *Finalize) errorQueue(originMessage *brokerv2.Message, errorQueueReason string) func() {
	return func() {
		if originMessage.Headers == nil {
			originMessage.Headers = make(map[string]any)
		}
		originMessage.Headers["error-queue-reason"] = errorQueueReason
		if err := app.broker.Publish(context.Background(), "error", *originMessage); err != nil {
			log.Errorf("failed to publish to error queue, reason: %v", err)
		}
	}
}
