// The verify service reads and decrypts ingested files from the archive
// storage and sends accession requests.
package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	verifyconfig "github.com/neicnordic/sensitive-data-archive/cmd/verify/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

type verify struct {
	db             database.Database
	broker         broker.Broker
	archiveReader  storage.Reader
	archiveKeyList []*[32]byte
	schemaPath     string
	routingKey     string
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

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-verify")
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

	app := &verify{
		schemaPath: verifyconfig.SchemaPath(),
		routingKey: verifyconfig.RoutingKey(),
	}

	app.db, err = postgres.NewPostgresSQLDatabase(ctx)
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
		return fmt.Errorf("failed to initialize mq broker: %v", err)
	}

	defer func() {
		if app.broker == nil {
			return
		}
		if err := app.broker.Close(); err != nil {
			slog.Warn("failed to close broker", slog.Any("error", err))
		}
	}()

	app.archiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil {
		return fmt.Errorf("failed to initialize archive reader, due to: %v", err)
	}
	app.archiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil || len(app.archiveKeyList) == 0 {
		return errors.New("no C4GH private keys configured")
	}

	consumeErr := make(chan error, 1)
	slog.Info("verify service started")
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, verifyconfig.SourceQueue(), app.handleMessage)
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	startupSpan.End()

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
				slog.Error("consumer failure during shutdown", slog.Any("error", err))
			}
		case sig := <-sigc:
			slog.Warn("received a second signal, not waiting for the running handler", slog.String("signal", sig.String()))
		}

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", slog.Any("error", err), slog.String("source-queue", verifyconfig.SourceQueue()))
			cancel()

			return err
		}

		return nil
	}
}

func (app *verify) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, span := observability.StartSpan(ctx, "handleMessage", attribute.String("message-key", message.Key))
	defer span.End()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.schemaPath), message.Body); err != nil {
		slog.Error("validation of incoming message failed", "error", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "validation of incoming message failed")}, nil
	}

	var ingestionVerification schema.IngestionVerification
	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(message.Body, &ingestionVerification)

	span.SetAttributes(
		attribute.String("file-id", ingestionVerification.FileID),
		attribute.String("file-path", ingestionVerification.FilePath),
		attribute.String("user", ingestionVerification.User),
	)

	// If the file has been canceled by the uploader, don't spend time working on it.
	status, err := app.db.GetFileStatus(ctx, ingestionVerification.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Error("file status not found", err)

			return []func(){app.errorQueue(ctx, message, "file status not found")}, nil
		}

		span.Warn("failed to get file status",
			slog.Any("error", err),
		)

		return nil, err
	}

	if status == "disabled" || status == "removed" {
		span.Info("file is disabled or removed, stopping verification", slog.String("status", status))

		return nil, nil
	}

	header, err := app.db.GetHeader(ctx, ingestionVerification.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Error("file header not found", err)

			return []func(){app.errorQueue(ctx, message, "file header not found")}, nil
		}

		span.Warn("failed to get file header", slog.Any("error", err))

		return nil, err
	}

	archiveLocation, err := app.db.GetArchiveLocation(ctx, ingestionVerification.FileID)
	if err != nil {
		span.Error("failed to get archive location", err)

		return nil, err
	}
	if archiveLocation == "" {
		span.Error("archive location for file not known", nil)

		if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error": "archive location for file not known"}`, string(message.Body)); err != nil {
			slog.Warn("failed to update file event log to error",
				slog.Any("error", err),
			)

			return nil, err
		}

		return []func(){app.errorQueue(ctx, message, "archive location for file not known")}, nil
	}

	file := new(database.FileInfo)
	file.Size, err = app.archiveReader.GetFileSize(ctx, archiveLocation, ingestionVerification.ArchivePath)
	if err != nil { //nolint:nestif
		span.Warn("failed to get file size from archive storage",
			slog.Any("error", err),
		)
		if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
			if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"file not found in archive storage"}`, string(message.Body)); err != nil {
				span.Warn("failed to update file event log to error", slog.Any("error", err))

				return nil, err
			}

			return []func(){app.errorQueue(ctx, message, "file not found in archive storage")}, nil
		}

		return nil, err
	}

	archivedChecksum := sha256.New()
	f, err := app.archiveReader.NewFileReader(ctx, archiveLocation, ingestionVerification.ArchivePath)
	if err != nil {
		span.Warn("failed to read file from archive storage", slog.Any("error", err))

		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	var key *[32]byte
	for _, k := range app.archiveKeyList {
		size, err := headers.EncryptedSegmentSize(header, *k)
		if (err == nil) && (size != 0) {
			key = k

			break
		}
	}

	if key == nil {
		span.Error("no matching key found for file", nil)

		if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"no matching c4gh key found for file"}`, string(message.Body)); err != nil {
			span.Warn("failed to update file event log to error", slog.Any("error", err))

			return nil, err
		}

		return nil, nil
	}

	mr := io.MultiReader(bytes.NewReader(header), io.TeeReader(f, archivedChecksum))
	c4ghr, err := streaming.NewCrypt4GHReader(mr, *key, nil)
	if err != nil {
		span.Warn("failed to open c4gh decryptor stream", slog.Any("error", err))

		return nil, err
	}
	defer func() {
		if err := c4ghr.Close(); err != nil {
			span.Warn("failed to close crypt4gh reader", slog.Any("error", err))
		}
	}()

	md5hash := md5.New()
	decryptedChecksum := sha256.New()
	stream := io.TeeReader(c4ghr, md5hash)

	if file.DecryptedSize, err = io.Copy(decryptedChecksum, stream); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			span.Warn("context canceled while decrypting file")

			return nil, err
		}
		span.Error("failed to copy decrypted data", err)

		return []func(){app.errorQueue(ctx, message, "failed to copy decrypted data")}, nil
	}

	// At this point we should do checksum comparison
	file.ArchivedChecksum = fmt.Sprintf("%x", archivedChecksum.Sum(nil))
	file.DecryptedChecksum = fmt.Sprintf("%x", decryptedChecksum.Sum(nil))

	switch {
	case ingestionVerification.ReVerify:
		decrypted, err := app.db.GetDecryptedChecksum(ctx, ingestionVerification.FileID)
		if err != nil {
			span.Warn("failed to get unencrypted checksum for file", slog.Any("error", err))

			return nil, err
		}

		if file.DecryptedChecksum != decrypted {
			span.Error("decrypted checksum don't match for file", nil)
			if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"decrypted checksum don't match"}`, string(message.Body)); err != nil {
				span.Warn("failed to update file event log to error", slog.Any("error", err))

				return nil, err
			}

			return nil, nil
		}

		if file.ArchivedChecksum != ingestionVerification.EncryptedChecksums[0].Value {
			span.Error("archived checksum don't match for file", nil)
			if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"archived checksum don't match"}`, string(message.Body)); err != nil {
				span.Warn("failed to update file event log to error", slog.Any("error", err))

				return nil, err
			}

			return nil, nil
		}

		return nil, nil
	default:
	}

	c := schema.IngestionAccessionRequest{
		User:     ingestionVerification.User,
		FilePath: ingestionVerification.FilePath,
		DecryptedChecksums: []schema.Checksums{
			{Type: "sha256", Value: fmt.Sprintf("%x", decryptedChecksum.Sum(nil))},
			{Type: "md5", Value: fmt.Sprintf("%x", md5hash.Sum(nil))},
		},
	}

	verifiedMessage, _ := json.Marshal(&c)
	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession-request.json", app.schemaPath), verifiedMessage)
	if err != nil {
		span.Error("validation of outgoing message failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "validation of outgoing message failed")}, nil
	}

	storedFileInfo, err := app.db.GetFileInfo(ctx, ingestionVerification.FileID)
	if err != nil {
		span.Warn("failed to get file info", slog.Any("error", err))

		return nil, err
	}

	if storedFileInfo.DecryptedChecksum != "" && storedFileInfo.DecryptedChecksum != file.DecryptedChecksum {
		// This indicates that the file has been verified previously and reuploaded & ingested without first being cancelled

		span.Error("decrypted checksum don't match for file", nil)
		if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"decrypted checksum don't match"}`, string(message.Body)); err != nil {
			span.Warn("failed to update file event log to error", slog.Any("error", err))

			return nil, err
		}

		return nil, nil
	}

	if storedFileInfo.ArchivedChecksum != "" && storedFileInfo.ArchivedChecksum != file.ArchivedChecksum {
		// This indicates that the file has been verified previously then reuploaded & ingested without first being cancelled
		span.Error("archived checksum don't match for file", nil)
		if err := app.db.UpdateFileEventLog(ctx, ingestionVerification.FileID, "error", "verify", `{"error":"archived checksum don't match"}`, string(message.Body)); err != nil {
			span.Warn("failed to update file event log to error", slog.Any("error", err))

			return nil, err
		}

		return nil, nil
	}

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

	// Here we check the file status again to ensure it has not been canceled or removed during the verification
	// and we don't override that with a verified file event
	status, err = tx.GetFileStatus(ctx, ingestionVerification.FileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Error("file status not found after verification", err)

			return []func(){app.errorQueue(ctx, message, "file status not found after verification")}, nil
		}

		span.Warn("failed to get file status after verification", slog.Any("error", err))

		return nil, err
	}

	if status == "disabled" || status == "removed" {
		span.Info("file was disabled or removed during verification", slog.String("status", status))

		return nil, nil
	}

	if storedFileInfo.DecryptedChecksum == "" || storedFileInfo.ArchivedChecksum == "" {
		if err := tx.SetVerified(ctx, file, ingestionVerification.FileID); err != nil {
			span.Warn("failed to set file as verified", slog.Any("error", err))

			return nil, err
		}
	}

	if err := tx.UpdateFileEventLog(ctx, ingestionVerification.FileID, "verified", "verify", "{}", string(verifiedMessage)); err != nil {
		span.Warn("failed to update file event log to verified", slog.Any("error", err))

		return nil, err
	}

	if err := tx.Commit(); err != nil {
		span.Warn("failed to commit transaction", slog.Any("error", err))
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	// Publish verified message
	if err := app.broker.Publish(ctx, app.routingKey, broker.Message{
		Key:  ingestionVerification.FileID,
		Body: verifiedMessage,
	}); err != nil {
		span.Error("failed to publish verified message",
			err,
			slog.String("routing-key", app.routingKey),
		)

		return nil, err
	}

	return nil, nil
}

func (app *verify) errorQueue(ctx context.Context, originMessage *broker.Message, errorQueueReason string) func() {
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
