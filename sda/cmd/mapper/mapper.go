// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	mapperconf "github.com/neicnordic/sensitive-data-archive/cmd/mapper/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/pkg/observability"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

type mapper struct {
	db          database.Database
	inboxWriter storage.Writer
	broker      broker.Broker
	inboxConfig helper.InboxProjectConfig
	schemaPath  string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := configv2.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-mapper")
	if err != nil {
		return fmt.Errorf("failed to setup OTel SDK: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shutdown OTel SDK", "err", err)
		}
		shutdownCancel()
	}()

	ctx, startupSpan := observability.StartSpan(ctx, "start up")
	defer startupSpan.End()

	app := &mapper{
		db:          nil,
		inboxWriter: nil,
		broker:      nil,
		inboxConfig: helper.InboxProjectConfig{},
		schemaPath:  mapperconf.SchemaPath(),
	}

	app.inboxConfig, err = config.LoadInboxProjectConfig()
	if err != nil {
		return fmt.Errorf("failed to load inbox project config: %v", err)
	}

	app.db, err = postgres.NewPostgresSQLDatabase(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer func() {
		if err := app.db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}
	}()
	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 28 {
		return errors.Join(errors.New("database schema v28 is required"), err)
	}

	app.broker, err = rabbitmq.NewRabbitMQBroker(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer func() {
		if app.broker == nil {
			return
		}
		if err := app.broker.Close(); err != nil {
			slog.Error("could not close broker", "error", err)
		}
	}()

	lb, err := locationbroker.NewLocationBroker(app.db)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker, due to: %v", err)
	}
	app.inboxWriter, err = storage.NewWriter(ctx, "inbox", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize inbox writer, due to: %v", err)
	}

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, mapperconf.SourceQueue(), app.handleMessage)
	}()
	slog.Info("mapper service started")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	startupSpan.End()

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()

		// Subscribe returns once the handler that was running has finished
		// and acked its message; broker.shutdown_grace cancels the handler's
		// context after that long, but the handler decides when it returns.
		// A second signal skips the wait.
		select {
		case err := <-consumeErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("consumer failure during shutdown", slog.Any("error", err))
			}
		case sig := <-sigc:
			slog.Warn("received a second signal, not waiting for the running handler", slog.String("signal", sig.String()))
		}

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", slog.Any("error", err), slog.String("source-queue", mapperconf.SourceQueue()))
			cancel()

			return err
		}

		return nil
	}
}

func (app *mapper) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, span := observability.StartSpan(ctx, "handleMessage", attribute.String("message-key", message.Key))
	defer span.End()

	operation, err := datasetOperationFromMessage(message.Body)
	if err != nil {
		span.Error("could not derive schema from message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "could not derive schema from message")}, nil
	}

	switch operation {
	case "mapping":
		return app.handleMapping(ctx, message)
	case "release":
		return app.handleRelease(ctx, message)
	case "deprecate":
		return app.handleDeprecate(ctx, message)
	case "file_removal":
		return app.handleFileRemoval(ctx, message)
	default:
		return []func(){app.errorQueue(ctx, message, "unknown operation type")}, nil
	}
}

func (app *mapper) handleMapping(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "handleMapping")
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", app.schemaPath), message.Body); err != nil {
		span.Error("incoming message validation failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "incoming message validation failed")}, nil
	}

	var mappings schema.DatasetMapping
	if err := json.Unmarshal(message.Body, &mappings); err != nil {
		span.Error("failed to unmarshal incoming message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "failed to unmarshal incoming message")}, nil
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		span.Warn("failed to begin transaction", slog.Any("error", err))

		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			span.Error("failed to rollback transaction", err)
		}
	}()

	var filesToCleanFromInbox []*database.MappingData

	for _, fileAccession := range mappings.AccessionIDs {
		var fileDownloadPath *string

		if mappings.FileDownloadPaths != nil {
			if v, ok := mappings.FileDownloadPaths[fileAccession]; ok && v != "" {
				fileDownloadPath = &v
			}
		}

		span.Debug("mapping file to dataset",
			slog.String("dataset-id", mappings.DatasetID),
			slog.String("file-accession", fileAccession),
			slog.Bool("overridden-download-path", fileDownloadPath != nil),
		)
		fileMappingData, err := tx.GetMappingData(ctx, fileAccession)
		if err != nil {
			span.Warn("failed to get mapping data of file",
				slog.String("file-accession", fileAccession),
				slog.String("dataset-id", mappings.DatasetID),
				slog.Any("error", err),
			)

			return nil, err
		}

		if fileMappingData == nil {
			span.Error("mapping data for file not found",
				nil,
				slog.String("file-accession", fileAccession),
			)

			// send message to error queue and do not requeue
			return []func(){app.errorQueue(ctx, message, "mapping data for file not found")}, nil
		}
		if err := tx.MapFileToDataset(ctx, mappings.DatasetID, fileMappingData.FileID, fileDownloadPath); err != nil {
			span.Error("failed to map file to dataset-id",
				err,
				slog.String("file-accession", fileAccession),
			)

			if errors.Is(err, database.ErrUniqueViolation) {
				return []func(){app.errorQueue(ctx, message, "mapping violates unique constraint")}, nil
			}

			return nil, err
		}

		if fileMappingData.SubmissionLocation == "" {
			span.Warn("file does not have a known submission location, can not remove file from inbox",
				slog.String("file-id", fileMappingData.FileID),
			)

			continue
		}

		filesToCleanFromInbox = append(filesToCleanFromInbox, fileMappingData)
	}

	if err := tx.UpdateDatasetEvent(ctx, mappings.DatasetID, "registered", string(message.Body)); err != nil {
		span.Error("failed to update dataset status to registered", err)

		if errors.Is(err, database.ErrForeignKeyViolation) {
			return []func(){app.errorQueue(ctx, message, "mapping violates foreign key constraint")}, nil
		}

		return nil, err
	}

	if err := tx.Commit(); err != nil {
		span.Error("failed to commit transaction", err)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	for _, fileMappingData := range filesToCleanFromInbox {
		resolvedSubmissionPath := helper.ResolveInboxPath(fileMappingData.SubmissionFilePath, fileMappingData.User, app.inboxConfig)
		if err := app.inboxWriter.RemoveFile(ctx, fileMappingData.SubmissionLocation, resolvedSubmissionPath); err != nil {
			span.Warn("failed to remove file from inbox",
				slog.String("file-id", fileMappingData.FileID),
				slog.String("submission-path", fileMappingData.SubmissionFilePath),
				slog.String("submission-location", fileMappingData.SubmissionLocation),
				slog.Any("error", err),
			)
		}
	}

	return nil, nil
}

func (app *mapper) handleRelease(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "handleRelease")
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", app.schemaPath), message.Body); err != nil {
		span.Error("incoming message validation failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "incoming message validation failed")}, nil
	}

	var datasetRelease schema.DatasetRelease
	if err := json.Unmarshal(message.Body, &datasetRelease); err != nil {
		span.Error("failed to unmarshal incoming message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "failed to unmarshal incoming message")}, nil
	}

	if err := app.db.UpdateDatasetEvent(ctx, datasetRelease.DatasetID, "released", string(message.Body)); err != nil {
		span.Error("failed to update dataset status", err)

		if errors.Is(err, database.ErrForeignKeyViolation) {
			return []func(){app.errorQueue(ctx, message, "mapping violates foreign key constraint")}, nil
		}

		return nil, err
	}

	return nil, nil
}

func (app *mapper) handleDeprecate(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "handleDeprecate")
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-deprecate.json", app.schemaPath), message.Body); err != nil {
		span.Error("incoming message validation failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "incoming message validation failed")}, nil
	}

	var datasetDeprecate schema.DatasetDeprecate
	if err := json.Unmarshal(message.Body, &datasetDeprecate); err != nil {
		span.Error("failed to unmarshal incoming message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "failed to unmarshal incoming message")}, nil
	}

	if err := app.db.UpdateDatasetEvent(ctx, datasetDeprecate.DatasetID, "deprecated", string(message.Body)); err != nil {
		span.Error("failed to update dataset status", err)

		if errors.Is(err, database.ErrForeignKeyViolation) {
			return []func(){app.errorQueue(ctx, message, "mapping violates foreign key constraint")}, nil
		}

		return nil, err
	}

	return nil, nil
}

func (app *mapper) handleFileRemoval(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, span := observability.StartSpan(ctx, "handleFileRemoval")
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-file-removal.json", app.schemaPath), message.Body); err != nil {
		span.Error("incoming message validation failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "incoming message validation failed")}, nil
	}

	var datasetFileRemoval schema.DatasetFileRemoval
	if err := json.Unmarshal(message.Body, &datasetFileRemoval); err != nil {
		span.Error("failed to unmarshal incoming message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "failed to unmarshal incoming message")}, nil
	}

	span.SetAttributes(
		attribute.String("dataset_id", datasetFileRemoval.DatasetID),
		attribute.String("accession_id", datasetFileRemoval.AccessionID),
	)

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		span.Warn("failed to begin transaction", slog.Any("error", err))

		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			span.Error("failed to rollback transaction", err)
		}
	}()

	datasetStatus, err := tx.GetDatasetStatus(ctx, datasetFileRemoval.DatasetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Error("dataset does not exists", err)

			return []func(){app.errorQueue(ctx, message, "dataset does not exists")}, nil
		}
		span.Warn("failed to get dataset status", slog.Any("error", err))

		return nil, err
	}

	if err := tx.RemoveFileFromDataset(ctx, datasetFileRemoval.DatasetID, datasetFileRemoval.AccessionID); err != nil {
		if !errors.Is(err, database.ErrNoRowsAffected) {
			span.Warn("failed to remove file from dataset", slog.Any("error", err))

			return nil, err
		}

		span.Info("file was not associated with dataset")

		return nil, nil
	}

	targetStatus := "file_removed"

	// if dataset is deprecated allow file removal but do not "undeprecate" dataset
	if datasetStatus == "deprecated" {
		targetStatus = datasetStatus
	}

	if err := tx.UpdateDatasetEvent(ctx, datasetFileRemoval.DatasetID, targetStatus, string(message.Body)); err != nil {
		span.Warn("failed to log file removal dataset event", slog.Any("error", err))

		return nil, err
	}

	if err := tx.Commit(); err != nil {
		span.Error("failed to commit transaction", err)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	return nil, nil
}

// schemaFromDatasetOperation returns the operation done with dataset supplied in body of the message
func datasetOperationFromMessage(body []byte) (string, error) {
	message := make(map[string]any)
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	datasetMessageType, ok := message["type"]
	if !ok {
		return "", errors.New("malformed message, dataset message type is missing")
	}

	datasetOpsType, ok := datasetMessageType.(string)
	if !ok {
		return "", errors.New("could not cast operation attribute to string")
	}

	return datasetOpsType, nil
}

func (app *mapper) errorQueue(ctx context.Context, originMessage *broker.Message, errorQueueReason string) func() {
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
