// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/model/headers"
	syncconf "github.com/neicnordic/sensitive-data-archive/cmd/sync/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

type sync struct {
	archiveC4ghPrivateKey, syncC4ghPubKey *[32]byte
	db                                    database.Database
	broker                                broker.Broker
	archiveReader                         storage.Reader
	syncWriter                            storage.Writer
	schemaPath                            string
	syncDatasetWithPrefix                 string
	remoteURL, remoteUser, remotePassword string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	app := &sync{
		schemaPath:            syncconf.SchemaPath(),
		syncDatasetWithPrefix: syncconf.SyncDatasetWithPrefix(),
		remoteURL:             syncconf.RemoteURL(),
		remoteUser:            syncconf.RemoteUser(),
		remotePassword:        syncconf.RemotePassword(),
	}

	var err error
	app.syncC4ghPubKey, err = config.GetC4GHPublicKey(syncconf.SyncC4ghPubKeyPath())
	if err != nil {
		return fmt.Errorf("failed to get sync c4gh pub key from config, due to: %v", err)
	}

	app.archiveC4ghPrivateKey, err = config.GetC4GHKey()
	if err != nil {
		return fmt.Errorf("failed to get c4gh key from config, due to: %v", err)
	}

	app.db, err = postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer func() {
		if err := app.db.Close(); err != nil {
			slog.Warn("failed to close database", slog.Any("error", err))
		}
	}()
	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
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
			slog.Warn("failed to close broker", slog.Any("error", err))
		}
	}()

	lb, err := locationbroker.NewLocationBroker(app.db)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker, due to: %v", err)
	}
	app.syncWriter, err = storage.NewWriter(ctx, "sync", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize sync writer, due to: %v", err)
	}
	app.archiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil {
		return fmt.Errorf("failed to initialize archive reader, due to: %v", err)
	}

	app.archiveC4ghPrivateKey, err = config.GetC4GHKey()
	if err != nil {
		return fmt.Errorf("failed to get c4gh key from config, due to: %v", err)
	}

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, syncconf.SourceQueue(), app.handleMessage)
	}()
	slog.Info("sync service started")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", "error", err, "source-queue", syncconf.SourceQueue())
			cancel()

			return err
		}

		return nil
	}
}

func (app *sync) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	slog.Debug("received message", slog.String("message-key", message.Key))

	operationType, err := schemaFromDatasetOperation(message.Body)
	if err != nil {
		slog.Error("failed to parse dataset operation from incoming message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, fmt.Sprintf("failed to parse dataset operation from incoming message: %v", err))}, nil
	}

	if operationType != "mapping" {
		slog.Debug("skipping non dataset mapping operation",
			slog.String("message-key", message.Key),
			slog.String("operation", operationType),
		)

		return nil, nil
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", app.schemaPath), message.Body); err != nil {
		slog.Error("incoming message validation failed", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, fmt.Sprintf("incoming message validation failed: %v", err))}, nil
	}

	var datasetMapping schema.DatasetMapping
	// we unmarshal the message in the validation step so this is safe to do
	if err := json.Unmarshal(message.Body, &datasetMapping); err != nil {
		slog.Error("failed to unmarshal incoming message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, fmt.Sprintf("failed to unmarshal incoming message: %v", err))}, nil
	}

	if !strings.HasPrefix(datasetMapping.DatasetID, app.syncDatasetWithPrefix) {
		slog.Info("external dataset", slog.String("dataset-id", datasetMapping.DatasetID))

		return nil, nil
	}

	for _, fileAccession := range datasetMapping.AccessionIDs {
		if err := app.syncFile(ctx, fileAccession); err != nil {
			// Non retryable errors
			if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) ||
				errors.Is(err, sql.ErrNoRows) ||
				strings.Contains(err.Error(), "failed to reencrypt header") {
				slog.Error("could not sync file",
					slog.String("accession-id", fileAccession),
					slog.Any("error", err),
				)

				return []func(){app.errorQueue(message, fmt.Sprintf("could not sync file %s: %v", fileAccession, err))}, nil
			}
			slog.Warn("failed to sync archived file",
				slog.String("accession-id", fileAccession),
				slog.Any("error", err),
			)

			// Here we reconsume message, as we don't expect error when syncing file
			// possible that if some files from message are synced, and then reconsume that sync storage will have duplicate files in different locations
			return nil, err
		}
	}

	if app.remoteURL == "" {
		return nil, nil
	}

	if err := app.sendHTTPNotification(ctx, datasetMapping); err != nil {
		slog.Error("failed to send http sync notification",
			slog.Any("error", err),
			slog.Any("message-key", message.Key),
		)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, fmt.Sprintf("failed to send http sync notification: %v", err))}, nil
	}

	return nil, nil
}

func (app *sync) syncFile(ctx context.Context, accessionID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	inboxPath, err := app.db.GetInboxPath(ctx, accessionID)
	if err != nil {
		return fmt.Errorf("failed to get inbox path, reason: %w", err)
	}

	archivePath, archiveLocation, err := app.db.GetArchivePathAndLocation(ctx, accessionID)
	if err != nil {
		return fmt.Errorf("failed to get archive path and location, reason: %w", err)
	}

	fileSize, err := app.archiveReader.GetFileSize(ctx, archiveLocation, archivePath)
	if err != nil {
		return fmt.Errorf("failed to get file size from archive storage, location: %s, path: %s, reason: %w", archiveLocation, archivePath, err)
	}

	file, err := app.archiveReader.NewFileReader(ctx, archiveLocation, archivePath)
	if err != nil {
		return fmt.Errorf("failed to read file from archive storage, location: %s, path: %s, reason: %w", archiveLocation, archivePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := app.db.GetHeaderByAccessionID(ctx, accessionID)
	if err != nil {
		return fmt.Errorf("failed to get header from db, reason: %w", err)
	}

	newHeader, err := headers.ReEncryptHeader(header, *app.archiveC4ghPrivateKey, [][chacha20poly1305.KeySize]byte{*app.syncC4ghPubKey})
	if err != nil {
		return fmt.Errorf("failed to reencrypt header, reason: %w", err)
	}

	contentReader, contentWriter := io.Pipe()

	go func() {
		defer func() {
			_ = contentWriter.Close()
		}()
		if _, err := contentWriter.Write(newHeader); err != nil {
			_ = contentWriter.CloseWithError(fmt.Errorf("failed to write header, reason: %w", err))

			return
		}
		if copiedSize, err := io.Copy(contentWriter, file); err != nil {
			_ = contentWriter.CloseWithError(fmt.Errorf("failed to write file content, reason: %w", err))
		} else if copiedSize != fileSize {
			_ = contentWriter.CloseWithError(errors.New("copied size does not match file size"))
		}
	}()

	_, err = app.syncWriter.WriteFile(ctx, inboxPath, contentReader)
	if err != nil {
		return fmt.Errorf("failed to upload file to storage, reason: %w", err)
	}
	_ = contentReader.Close()

	return nil
}

func (app *sync) buildSyncDatasetJSON(ctx context.Context, datasetMapping schema.DatasetMapping) ([]byte, error) {
	var dataset = schema.SyncDataset{
		DatasetID: datasetMapping.DatasetID,
	}

	for _, ID := range datasetMapping.AccessionIDs {
		data, err := app.db.GetSyncData(ctx, ID)
		if err != nil {
			return nil, err
		}
		datasetFile := schema.DatasetFiles{
			FilePath: data.FilePath,
			FileID:   ID,
			ShaSum:   data.Checksum,
		}
		dataset.DatasetFiles = append(dataset.DatasetFiles, datasetFile)
		dataset.User = data.User
	}

	datasetJSON, err := json.Marshal(dataset)
	if err != nil {
		return nil, err
	}

	return datasetJSON, nil
}

func (app *sync) sendHTTPNotification(ctx context.Context, datasetMapping schema.DatasetMapping) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	payload, err := app.buildSyncDatasetJSON(ctx, datasetMapping)
	if err != nil {
		slog.Error("failed to build SyncDatasetJSON", slog.Any("error", err))

		return err
	}

	req, err := http.NewRequest(http.MethodPost, app.remoteURL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if app.remoteUser != "" && app.remotePassword != "" {
		req.SetBasicAuth(app.remoteUser, app.remotePassword)
	}
	resp, err := client.Do(req) // #nosec G704 host originates from configuration
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}

	return nil
}

func (app *sync) errorQueue(originMessage *broker.Message, errorQueueReason string) func() {
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

	return datasetOpsType, nil
}
