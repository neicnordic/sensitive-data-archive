// The finalize command accepts messages with accessionIDs for
// ingested files and registers them in the database.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	appconf "github.com/neicnordic/sensitive-data-archive/cmd/finalize/config"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"go.opentelemetry.io/otel/attribute"

	log "github.com/sirupsen/logrus"
)

// cleanupTimeout bounds the removal of a freshly written backup object after a failed backup.
const cleanupTimeout = 30 * time.Second

// errFileCancelled is returned by backupFile when the file was cancelled while it was being copied.
var errFileCancelled = errors.New("file was cancelled during backup")

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

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-finalize")
	if err != nil {
		return fmt.Errorf("failed to setup OTel SDK: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			slog.Error("failed to shutdown OTel SDK", "err", err)
		}
	}()
	ctx, startupSpan := observability.StartSpan(ctx, "start up")
	defer startupSpan.End()

	app.db, err = postgres.NewPostgresSQLDatabase(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer func() {
		if err := app.db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}
	}()

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
			slog.Error("could not close broker", slog.Any("error", err))
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
		slog.Warn("archive or backup destination not configured, backup will not be performed.")
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	startupSpan.End()

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, appconf.SourceQueue(), app.handleMessage)
	}()
	slog.Info("finalize service started")

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", slog.String("signal", sig.String()))
		cancel()

		// Subscribe returns once the handler that was running has finished
		// and acked its message; broker.shutdown_grace cancels the handler's
		// context after that long, but the handler decides when it returns.
		// A second signal skips the wait.
		select {
		case err := <-consumeErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("consumer failure during shutdown", "error", err)
			}
		case sig := <-sigc:
			slog.Warn("received a second signal, not waiting for the running handler", "signal", sig)
		}

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", slog.Any("error", err))
			cancel()

			return err
		}

		return nil
	}
}

func (app *Finalize) handleMessage(ctx context.Context, message *brokerv2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, span := observability.StartSpan(ctx, "handleMessage", attribute.String("message-key", message.Key))
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", appconf.SchemaPath()), message.Body); err != nil {
		span.Error("validation of incoming message (ingestion-accession) failed", err)

		return []func(){app.errorQueue(ctx, message, "could not validate message")}, nil
	}

	var ingestionAccession schema.IngestionAccession
	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(message.Body, &ingestionAccession)
	// If the file has been canceled by the uploader, don't spend time working on it.
	status, err := app.db.GetFileStatus(ctx, message.Key)
	if err != nil {
		span.Warn("failed to get file status", slog.Any("error", err))

		if errors.Is(err, sql.ErrNoRows) {
			return []func(){app.errorQueue(ctx, message, "file not recognized")}, nil
		}

		return nil, err
	}

	var callbacks []func()
	switch status {
	case "":
		return []func(){app.errorQueue(ctx, message, "file not recognized")}, nil
	case "disabled", "removed":
		span.Debug("file is disabled or removed, aborting work")

		return nil, nil
	case "verified", "enabled", "backed up":
		callbacks, err = app.setAccession(ctx, &ingestionAccession, message)
	case "ready":
		span.Debug("file is already marked as ready")

		// Here we send completion message again if file is already marked as ready
		// This is to protect against scenarios where the setAccession transaction updating file was successful
		// but publishing the message failed meaning it was not delivered to the broker
		if err := app.sendCompleted(ctx, message.Key, &ingestionAccession); err != nil {
			return nil, err
		}

		return nil, nil
	default:
		span.Warn("file is not verified yet, aborting work")

		return nil, errors.New("file is not verified yet, aborting work")
	}

	return callbacks, err
}

func (app *Finalize) backupFile(ctx context.Context, message *brokerv2.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "backupFile", attribute.String("file-id", message.Key))
	defer span.End()

	archiveData, err := app.db.GetArchived(ctx, message.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get file archive information, reason: %v", err)
	}

	if archiveData == nil {
		return nil, errors.New("file archive information not found")
	}

	if archiveData.BackupLocation != "" && archiveData.BackupFilePath != "" {
		span.Info("skipping backup, file already backed up")

		return nil, nil
	}

	// Get size on disk, will also give some time for the file to appear if it has not already
	diskFileSize, err := app.archiveReader.GetFileSize(ctx, archiveData.Location, archiveData.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get size info for archived file, reason: %v", err)
	}

	if diskFileSize != archiveData.FileSize {
		return []func(){app.errorQueue(ctx, message, "archive file size does not match registered file size")}, fmt.Errorf("archive file size does not match registered file size, (disk size: %d, db size: %d)", diskFileSize, archiveData.FileSize)
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

	// From here until the commit is attempted the backup holds an object without a database
	// record. Remove it before the message is requeued so a retry does not leave an orphan
	// behind. Once Commit has been called the outcome is unknown (Postgres may have committed
	// even if the reply was lost), so the object is kept: an orphan is recoverable, a dangling
	// database record is not. The cleanup runs on a context that survives shutdown, bounded
	// so a hung storage endpoint can not block the handler from returning.
	commitAttempted := false
	defer func() {
		if commitAttempted {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if err := app.backupWriter.RemoveFile(cleanupCtx, backupLocation, archiveData.FilePath); err != nil {
			span.Error("failed to remove file from backup during rollback", err, slog.String("location", backupLocation))
		}
	}()

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		span.Warn("failed to begin transaction", slog.Any("error", err))
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			span.Error("failed to rollback transaction", err)
		}
	}()

	// Mark file as "backed up" and populate backup path and location
	if err := tx.SetBackedUp(ctx, backupLocation, archiveData.FilePath, message.Key); err != nil {
		return nil, fmt.Errorf("SetBackedUp failed, reason: (%w)", err)
	}

	// SetBackedUp holds the row lock on the file, so a cancel that landed while the backup was
	// copied is visible here and one that arrives later waits until this transaction is done.
	// The file must not be marked as backed up, and the copy is removed by the deferred cleanup.
	status, err := tx.GetFileStatus(ctx, message.Key)
	if err != nil {
		return nil, fmt.Errorf("GetFileStatus failed, reason: (%w)", err)
	}
	if status == "disabled" || status == "removed" {
		return nil, errFileCancelled
	}

	if err := tx.UpdateFileEventLog(ctx, message.Key, "backed up", "finalize", "{}", string(message.Body)); err != nil {
		return nil, fmt.Errorf("UpdateFileEventLog failed, reason: (%w)", err)
	}

	commitAttempted = true
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil, nil
}

func (app *Finalize) setAccession(ctx context.Context, ingestionAccession *schema.IngestionAccession, message *brokerv2.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "setAccession", attribute.String("file-id", message.Key), attribute.String("file-accession-id", ingestionAccession.AccessionID))
	defer span.End()

	accessionIDExists, err := app.db.CheckAccessionIDExists(ctx, ingestionAccession.AccessionID, message.Key)
	if err != nil {
		span.Warn("CheckAccessionIdExists failed", slog.Any("error", err))

		return nil, err
	}

	if accessionIDExists == "duplicate" {
		span.Error("accession ID already exists in the system", nil)
		// Send the message to an error queue so it can be analyzed.
		return []func(){app.errorQueue(ctx, message, "Duplicate accession ID")}, nil
	}

	if app.archiveReader != nil && app.backupWriter != nil {
		// The backup is committed in its own transaction. If setting the accession fails
		// afterwards the message is requeued, and the retry skips the copy because the
		// backup location is already recorded.
		callbacks, err := app.backupFile(ctx, message)
		switch {
		case errors.Is(err, errFileCancelled):
			span.Warn("file was cancelled during backup, aborting work")

			return nil, nil
		case err != nil:
			span.Error("failed to backup file", err)

			return callbacks, err
		}
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		span.Error("failed to begin transaction", err)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			span.Error("failed to rollback transaction", err)
		}
	}()

	if accessionIDExists == "same" {
		span.Info("file already has an accession ID, marking it as ready")
	}

	// SetAccessionID is always run, also when the accession ID is already set, because the
	// update holds the row lock on the file for the rest of the transaction. The status read
	// below then can not be overtaken by a cancel that commits between the backup and here.
	if err := tx.SetAccessionID(ctx, ingestionAccession.AccessionID, message.Key); err != nil {
		span.Error("failed to set accessionID for file", err)

		return nil, err
	}

	status, err := tx.GetFileStatus(ctx, message.Key)
	if err != nil {
		span.Warn("failed to get file status", slog.Any("error", err))

		return nil, err
	}
	if status == "disabled" || status == "removed" {
		span.Warn("file was cancelled during verification, aborting work")

		return nil, nil
	}

	if err := tx.UpdateFileEventLog(ctx, message.Key, "ready", "finalize", "{}", string(message.Body)); err != nil {
		span.Warn("set status ready failed", slog.Any("error", err))

		return nil, err
	}

	if err := tx.Commit(); err != nil {
		span.Error("failed to commit transaction", err)
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

func (app *Finalize) errorQueue(ctx context.Context, originMessage *brokerv2.Message, errorQueueReason string) func() {
	return func() {
		// Using context.WithoutCancel as this will run as a callback func after handleMessage ctx is canceled, but keeping context to start span under it
		ctx, span := observability.StartSpan(context.WithoutCancel(ctx), "errorQueue", attribute.String("error-queue-reason", errorQueueReason))
		defer span.End()

		if originMessage.Headers == nil {
			originMessage.Headers = make(map[string]any)
		}
		originMessage.Headers["error-queue-reason"] = errorQueueReason
		if err := app.broker.Publish(ctx, "error", *originMessage); err != nil {
			span.Error("failed to publish to error queue", err)
		}
	}
}
