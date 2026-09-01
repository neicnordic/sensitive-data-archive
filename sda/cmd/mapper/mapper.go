// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	log "github.com/sirupsen/logrus"
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

	app.db, err = postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer func() {
		_ = app.db.Close()
	}()
	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 25 {
		return errors.Join(errors.New("database schema v25 is required"), err)
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
	log.Info("mapper service started")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", "error", err, "source-queue", mapperconf.SourceQueue())
			cancel()

			return err
		}

		return nil
	}
}

func (app *mapper) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	schemaType, err := schemaFromDatasetOperation(message.Body)
	if err != nil {
		slog.Error("could not derive schema from message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "could not derive schema from message")}, nil
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/%s.json", app.schemaPath, schemaType), message.Body); err != nil {
		slog.Error("incoming message validation failed", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "incoming message validation failed")}, nil
	}

	var mappings schema.DatasetMapping
	// we unmarshal the message in the validation step so this is safe to do
	if err := json.Unmarshal(message.Body, &mappings); err != nil {
		slog.Error("failed to unmarshal incoming message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "failed to unmarshal incoming message")}, nil
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err, "dataset-id", mappings.DatasetID)

		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			slog.Error("failed to rollback transaction", "error", err, "dataset-id", mappings.DatasetID)
		}
	}()

	slog.Info("received mapping operation",
		slog.String("dataset-id", mappings.DatasetID),
		slog.String("operation", mappings.Type),
	)

	var filesToCleanFromInbox []*database.MappingData

	switch mappings.Type {
	case "mapping":
		for _, fileAccession := range mappings.AccessionIDs {
			var fileDownloadPath *string

			if mappings.FileDownloadPaths != nil {
				if v, ok := mappings.FileDownloadPaths[fileAccession]; ok && v != "" {
					fileDownloadPath = &v
				}
			}

			slog.Debug("mapping file to dataset",
				slog.String("dataset-id", mappings.DatasetID),
				slog.String("file-accession", fileAccession),
				slog.Bool("overriden-download-path", fileDownloadPath != nil),
			)
			fileMappingData, err := tx.GetMappingData(ctx, fileAccession)
			if err != nil {
				slog.Error("failed to get mapping data of file",
					slog.String("file-accession", fileAccession),
					slog.String("dataset-id", mappings.DatasetID),
					slog.Any("error", err),
				)

				return nil, err
			}

			if fileMappingData == nil {
				slog.Error("mapping data for file not found",
					slog.String("file-accession", fileAccession),
					slog.String("dataset-id", mappings.DatasetID),
				)

				// send message to error queue and do not requeue
				return []func(){app.errorQueue(message, "mapping data for file not found")}, nil
			}
			if err := tx.MapFileToDataset(ctx, mappings.DatasetID, fileMappingData.FileID, fileDownloadPath); err != nil {
				slog.Error("failed to map file to dataset-id",
					slog.String("file-accession", fileAccession),
					slog.String("dataset-id", mappings.DatasetID),
					slog.Any("error", err),
				)

				if errors.Is(err, database.ErrUniqueViolation) {
					return []func(){app.errorQueue(message, "mapping violates unique constraint")}, nil
				}

				return nil, err
			}

			if fileMappingData.SubmissionLocation == "" {
				slog.Warn("file does not have a known submission location, can not remove file from inbox",
					slog.String("file-id", fileMappingData.FileID),
				)

				continue
			}

			filesToCleanFromInbox = append(filesToCleanFromInbox, fileMappingData)
		}

		if err := tx.UpdateDatasetEvent(ctx, mappings.DatasetID, "registered", string(message.Body)); err != nil {
			slog.Error("failed to update dataset status",
				slog.String("dataset-id", mappings.DatasetID),
				slog.Any("error", err),
			)

			return nil, err
		}
	case "release":
		if err := tx.UpdateDatasetEvent(ctx, mappings.DatasetID, "released", string(message.Body)); err != nil {
			slog.Error("failed to update dataset status",
				slog.String("dataset-id", mappings.DatasetID),
				slog.Any("error", err),
			)

			return nil, err
		}
	case "deprecate":
		if err := tx.UpdateDatasetEvent(ctx, mappings.DatasetID, "deprecated", string(message.Body)); err != nil {
			slog.Error("failed to update dataset status",
				slog.String("dataset-id", mappings.DatasetID),
				slog.Any("error", err),
			)

			return nil, err
		}
	default:
		slog.Error("unknown mapping operation", slog.String("operation", mappings.Type))

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "unknown mapping type")}, nil
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit transaction", "error", err, "dataset-id", mappings.DatasetID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	for _, fileMappingData := range filesToCleanFromInbox {
		resolvedSubmissionPath := helper.ResolveInboxPath(fileMappingData.SubmissionFilePath, fileMappingData.User, app.inboxConfig)
		if err := app.inboxWriter.RemoveFile(ctx, fileMappingData.SubmissionLocation, resolvedSubmissionPath); err != nil {
			slog.Warn("failed to remove file from inbox",
				slog.String("file-id", fileMappingData.FileID),
				slog.String("submission-path", fileMappingData.SubmissionFilePath),
				slog.String("submission-location", fileMappingData.SubmissionLocation),
				slog.Any("error", err),
			)
		}
	}

	return nil, nil
}

// schemaFromDatasetOperation returns the operation done with dataset supplied in body of the message
func schemaFromDatasetOperation(body []byte) (string, error) {
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

	switch datasetOpsType {
	case "mapping":
		return "dataset-mapping", nil
	case "release":
		return "dataset-release", nil
	case "deprecate":
		return "dataset-deprecate", nil
	default:
		return "", errors.New("could not recognize mapping operation")
	}
}

func (app *mapper) errorQueue(originMessage *broker.Message, errorQueueReason string) func() {
	return func() {
		if originMessage.Headers == nil {
			originMessage.Headers = make(map[string]any)
		}
		originMessage.Headers["error-queue-reason"] = errorQueueReason
		if err := app.broker.Publish(context.Background(), "error", *originMessage); err != nil {
			slog.Error("failed to publish to error queue", "error", err, "message-key", originMessage.Key, "error-queue-reason", errorQueueReason)

			return
		}
		slog.Info("published message to error queue", "message-key", originMessage.Key, "error-queue-reason", errorQueueReason)
	}
}
