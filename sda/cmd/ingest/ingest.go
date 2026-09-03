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
	"hash"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	ingestconf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/inboxpath"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	log "github.com/sirupsen/logrus"
)

type Ingest struct {
	ArchiveWriter  storage.Writer
	BackupWriter   storage.Writer
	ArchiveReader  storage.Reader
	ArchiveKeyList []*[32]byte
	db             database.Database
	InboxReader    storage.Reader
	Broker         brokerv2.Broker
}

type decryptResult struct {
	keyHash    string
	hash       hash.Hash
	teedReader io.Reader
	header     []byte
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var err error
	app := Ingest{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err = configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}
	if err := inboxpath.Load(); err != nil {
		return fmt.Errorf("failed to load inbox path config: %v", err)
	}

	app.Broker, err = rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker: %v", err)
	}

	defer func() {
		if app.Broker == nil {
			return
		}
		if err := app.Broker.Close(); err != nil {
			slog.Error("could not close broker", "error", err)
		}
	}()

	app.db, err = postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db: %v", err)
	}
	defer app.db.Close()
	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	app.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil || len(app.ArchiveKeyList) == 0 {
		return errors.New("no C4GH private keys configured")
	}
	if err := app.registerC4GHKey(ctx); err != nil {
		return fmt.Errorf("failed to register c4gh key: %v", err)
	}
	storageLocationBroker, err := locationbroker.NewLocationBroker(app.db)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker: %v", err)
	}
	app.ArchiveWriter, err = storage.NewWriter(ctx, "archive", storageLocationBroker)
	if err != nil {
		return fmt.Errorf("failed to initialize archive writer: %v", err)
	}
	app.ArchiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil {
		return fmt.Errorf("failed to initialize archive reader: %v", err)
	}
	app.InboxReader, err = storage.NewReader(ctx, "inbox")
	if err != nil {
		return fmt.Errorf("failed to initialize inbox reader: %v", err)
	}

	backupWriter, err := storage.NewWriter(ctx, "backup", storageLocationBroker)
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidWriter) {
		return fmt.Errorf("failed to initialize backup writer: %v", err)
	}
	if backupWriter != nil {
		slog.Info("backup writer initialized, will clean cancelled files from backup storage")
		app.BackupWriter = backupWriter
	} else {
		slog.Info("no backup writer initialized, will NOT clean cancelled files from backup storage")
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.Broker.Subscribe(ctx, ingestconf.SourceQueue(), app.handleMessage)
	}()
	slog.Info("ingest service started")

	select {
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", "signal", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", "error", err, "source-queue", ingestconf.SourceQueue())
			cancel()

			return err
		}

		return nil
	}
}

func (app *Ingest) handleMessage(ctx context.Context, message *brokerv2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", ingestconf.SchemaPath()), message.Body); err != nil {
		slog.Error("could not validate message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "could not validate message")}, nil
	}

	var ingestionTrigger schema.IngestionTrigger

	if err := json.Unmarshal(message.Body, &ingestionTrigger); err != nil {
		slog.Error("could not unmarshal message", "error", err, "message-key", message.Key)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "could not unmarshal message")}, nil
	}
	slog.Info("received work", "message-key", message.Key, "filepath", ingestionTrigger.FilePath, "user", ingestionTrigger.User)

	var callbacks []func()
	var err error
	switch ingestionTrigger.Type {
	case "cancel":
		callbacks, err = app.cancelFile(ctx, message.Key, message)
	case "ingest":
		callbacks, err = app.ingestFile(ctx, message.Key, ingestionTrigger.FilePath, ingestionTrigger.User, ingestconf.ArchivedQueue(), message)
	default:
		slog.Warn("unknown ingest type", "type", ingestionTrigger.Type, "message-key", message.Key, "filepath", ingestionTrigger.FilePath, "user", ingestionTrigger.User)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "unknown ingest type")}, nil
	}

	return callbacks, err
}

func (app *Ingest) registerC4GHKey(ctx context.Context) error {
	h, err := app.db.ListKeyHashes(ctx)
	if err != nil {
		return err
	}
	if len(h) == 0 {
		for num, key := range app.ArchiveKeyList {
			publicKey := keys.DerivePublicKey(*key)
			if err := app.db.AddKeyHash(ctx, hex.EncodeToString(publicKey[:]), fmt.Sprintf("bootstrapped key: %d", num)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (app *Ingest) cancelFile(ctx context.Context, fileID string, message *brokerv2.Message) ([]func(), error) {
	fileExistsInDataset, err := app.db.IsFileInDataset(ctx, fileID)
	if err != nil {
		// requeue message as db error is not expected and should succeed on retries
		return nil, fmt.Errorf("failed to query db: %v", err)
	}

	if fileExistsInDataset {
		slog.Warn("cannot cancel file as it has been added to a dataset", "file-id", fileID)

		reason := "cannot cancel file: already added to a dataset"
		// send message to error queue, set file error event, and do not requeue
		return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
	}

	archiveData, err := app.db.GetArchived(ctx, fileID)
	if err != nil {
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	if archiveData == nil {
		slog.Warn("file not found in archive, skipping", "file-id", fileID)

		return nil, nil
	}

	if archiveData.Location != "" {
		if err := app.ArchiveWriter.RemoveFile(ctx, archiveData.Location, archiveData.FilePath); err != nil {
			// Just log error and continue as this should not block updating the db
			slog.Error("failed to remove file with from archive", "error", err, "file-id", fileID)
		}
	}

	if app.BackupWriter != nil && archiveData.BackupFilePath != "" && archiveData.BackupLocation != "" {
		if err := app.BackupWriter.RemoveFile(ctx, archiveData.BackupLocation, archiveData.BackupFilePath); err != nil {
			// Just log error and continue as this should not block updating the db
			slog.Error("failed to remove file from backup", "error", err, "file-id", fileID)
		}
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			slog.Error("failed to rollback transaction", "error", err, "file-id", fileID)
		}
	}()

	if err := tx.CancelFile(ctx, fileID, string(message.Body)); err != nil {
		slog.Error("failed to cancel file", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit transaction", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	slog.Info("file cancelled successfully", "file-id", fileID)

	return nil, nil
}

func (app *Ingest) ingestFile(ctx context.Context, fileID, filePath, user, archivedQueue string, message *brokerv2.Message) ([]func(), error) {
	status, err := app.db.GetFileStatus(ctx, fileID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("could not get file status for file", "error", err, "file-id", fileID)
		// requeue message as db error(other than sql.ErrNoRows) is not expected and should succeed on retries
		return nil, err
	}

	submissionLocation, err := app.db.GetSubmissionLocation(ctx, fileID)
	if err != nil {
		slog.Error("failed to get submission location for file", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			slog.Error("failed to rollback transaction", "error", err, "file-id", fileID)
		}
	}()

	switch status {
	case "uploaded", "disabled":

	case "":
		// Catch all for implementations inbox uploading that does not register the file in the DB, e.g. for those not using S3inbox or sftpInbox
		// Since we dont have the submission location in storage, we need to look through all configured storage locations.

		// message.Key is the broker correlation-id, not the submission path; use the trigger's
		// filePath and resolve it to the physical inbox path before locating it.
		submissionLocation, err = app.InboxReader.FindFile(ctx, inboxpath.ResolveInboxPath(filePath, user))
		if err != nil {
			slog.Error("failed to find submission location for file", "error", err, "file-id", fileID)

			if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
				// send message to error queue and do not requeue
				return []func(){app.errorQueue(message, "failed to find submission location for file")}, nil
			}

			return nil, err
		}

		// Store the anonymized submission path (filePath), not the correlation-id, so the mapper can resolve it back on cleanup.
		fileID, err = tx.RegisterFile(ctx, &fileID, submissionLocation, filePath, user)
		if err != nil {
			slog.Error("failed to register file", "error", err, "file-id", fileID)

			return nil, err
		}
		// File is now registered; fall through to read + decrypt + archive in a single pass, the same
		// way a pre-registered "uploaded" file is handled. Returning here would leave a non-s3inbox
		// upload stuck at "registered", so verify never runs.

	default:
		slog.Warn("received ingestion trigger for file with unexpected status", "file-id", fileID, "status", status)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(message, "unexpected file status for ingest trigger")}, nil
	}

	if submissionLocation == "" {
		reason := "file submission location not known"
		slog.Error(reason, "file-id", fileID)
		// send message to error queue, set file event log, and do not requeue
		return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
	}

	sourceReader, err := app.InboxReader.NewFileReader(ctx, submissionLocation, inboxpath.ResolveInboxPath(filePath, user))
	if err != nil {
		switch {
		case errors.Is(err, storageerrors.ErrorFileNotFoundInLocation):
			reason := "failed to find file in inbox when expected"
			slog.Error(reason, "file-id", fileID)
			// send message to error queue, set file event log, and do not requeue
			return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
		case errors.Is(err, storageerrors.ErrorInvalidLocation):
			reason := "file has invalid submission location"
			slog.Error(reason, "file-id", fileID, "submission-location", submissionLocation)
			// send message to error queue, set file event log, and do not requeue
			return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
		case errors.Is(err, storageerrors.ErrorNoEndpointConfiguredForLocation):
			reason := "file has submission location which is not configured"
			slog.Error(reason, "file-id", fileID, "submission-location", submissionLocation)
			// send message to error queue, set file event log, and do not requeue
			return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
		default:
			slog.Error("failed to read file", "error", err, "file-id", fileID)
			// requeue message as inbox error is not expected and should succeed on retries
			return nil, err
		}
	}
	defer func() {
		_ = sourceReader.Close()
	}()

	if err := tx.UpdateFileEventLog(ctx, fileID, "submitted", "ingest", "{}", string(message.Body)); err != nil {
		slog.Error("failed to update file event log", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	dr, err := app.decrypt(sourceReader)
	if err != nil {
		slog.Error("failed ingestion during decrypt and archive", "error", err, "file-id", fileID)
		// send message to error queue, set file event log, and do not requeue
		return []func(){app.errorQueue(message, err.Error()), app.setErrorEvent(err.Error(), message)}, nil
	}

	location, err := app.archive(ctx, tx, dr.keyHash, fileID, dr.header, dr.teedReader)
	if err != nil {
		slog.Error("failed to archive file", "error", err, "file-id", fileID)
		// requeue message as db error is not expected and should succeed on retries
		return nil, err
	}

	checksum := fmt.Sprintf("%x", dr.hash.Sum(nil))

	if err := app.finalizeDatabaseRecords(ctx, tx, fileID, location, checksum, message); err != nil {
		slog.Error("failed to finalize database records", "error", err, "file-id", fileID)
		// requeue message as error is not expected and should succeed on retries
		return nil, err
	}

	// If commit fails we've already uploaded the file to the archive
	// but we will still rollback and reconsume the message as that can be done again, and it would just overwrite it
	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit transaction for ingest action", "error", err, "file-id", fileID)
		// requeue message as broker error is not expected and should succeed on retries
		return nil, err
	}

	// We need to send message after the commit, since there is a race condition where verify would consume and start processing the message before the commit is actioned.
	// If notifyArchived fails we can not requeue the message as the db transaction has already been commited. Failure here would require manual intervertion.
	if err := app.notifyArchived(ctx, fileID, filePath, user, checksum, archivedQueue, message); err != nil {
		reason := "failed to notify archived"
		slog.Error(reason, "error", err, "file-id", fileID)

		// send message to error queue, set file event log, and do not requeue
		return []func(){app.errorQueue(message, reason), app.setErrorEvent(reason, message)}, nil
	}

	slog.Info("file ingested successfully", "file-id", fileID)

	return nil, nil
}

func (app *Ingest) decrypt(source io.ReadCloser) (decryptResult, error) {
	fileHash := sha256.New()
	teedReader := io.TeeReader(source, fileHash)
	var headerBuf bytes.Buffer
	headerTee := io.TeeReader(teedReader, &headerBuf)

	header, err := headers.ReadHeader(headerTee)
	if err != nil {
		return decryptResult{}, fmt.Errorf("failed to parse crypt4gh header: %v", err)
	}

	var validKey *[32]byte
	for _, key := range app.ArchiveKeyList {
		if _, err := streaming.NewCrypt4GHReader(bytes.NewReader(header), *key, nil); err == nil {
			validKey = key

			break
		}
	}

	if validKey == nil {
		return decryptResult{}, errors.New("no valid keys found to decrypt file")
	}

	publicKey := keys.DerivePublicKey(*validKey)
	keyHash := hex.EncodeToString(publicKey[:])

	return decryptResult{keyHash: keyHash, hash: fileHash, teedReader: teedReader, header: header}, err
}

func (app *Ingest) archive(ctx context.Context, tx database.Transaction, keyHash, fileID string, rawHeader []byte, reader io.Reader) (string, error) {
	if err := tx.SetKeyHash(ctx, keyHash, fileID); err != nil {
		return "", err
	}

	if err := tx.StoreHeader(ctx, rawHeader, fileID); err != nil {
		return "", err
	}

	location, err := app.ArchiveWriter.WriteFile(ctx, fileID, reader)
	if err != nil {
		return "", err
	}

	return location, nil
}

func (app *Ingest) finalizeDatabaseRecords(ctx context.Context, tx database.Transaction, fileID, location, checksum string, message *brokerv2.Message) error {
	fileSize, err := app.ArchiveReader.GetFileSize(ctx, location, fileID)
	if err != nil {
		return err
	}

	fileInfo := new(database.FileInfo)
	fileInfo.Path = fileID
	fileInfo.Size = fileSize
	fileInfo.UploadedChecksum = checksum

	if err := tx.SetArchived(ctx, location, fileInfo, fileID); err != nil {
		return fmt.Errorf("failed to mark file as archived, file-id: %s: %v", fileID, err)
	}

	if err := tx.UpdateFileEventLog(ctx, fileID, "archived", "ingest", "{}", string(message.Body)); err != nil {
		return fmt.Errorf("failed to update file event log, file-id: %s: %v", fileID, err)
	}

	return nil
}

func (app *Ingest) notifyArchived(ctx context.Context, fileID, filePath, user, checksum, archivedQueue string, message *brokerv2.Message) error {
	msg := schema.IngestionVerification{
		User:               user,
		FilePath:           filePath,
		FileID:             fileID,
		ArchivePath:        fileID,
		EncryptedChecksums: []schema.Checksums{{Type: "sha256", Value: checksum}},
	}

	messageBody, err := json.Marshal(&msg)
	if err != nil {
		return err
	}

	archivedMessage := brokerv2.Message{
		Key:  message.Key,
		Body: messageBody,
	}

	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", ingestconf.SchemaPath()), messageBody)
	if err != nil {
		slog.Error("could not validate message", "error", err, "message-key", message.Key)

		return err
	}

	return app.Broker.Publish(ctx, archivedQueue, archivedMessage)
}

func (app *Ingest) setErrorEvent(details string, message *brokerv2.Message) func() {
	return func() {
		detailsMap := map[string]string{
			"error": details,
		}

		detailsJSON, err := json.Marshal(detailsMap)
		if err != nil {
			slog.Error("failed to marshal details to JSON", "error", err)
			detailsJSON = []byte("{}")
		}
		if err := app.db.UpdateFileEventLog(context.Background(), message.Key, "error", "ingest", string(detailsJSON), string(message.Body)); err != nil {
			slog.Error("failed to set file event log error event", "error", err)
		}
	}
}

func (app *Ingest) errorQueue(originMessage *brokerv2.Message, errorQueueReason string) func() {
	return func() {
		if originMessage.Headers == nil {
			originMessage.Headers = make(map[string]any)
		}
		originMessage.Headers["error-queue-reason"] = errorQueueReason
		if err := app.Broker.Publish(context.Background(), "error", *originMessage); err != nil {
			slog.Error("failed to publish to error queue", "error", err, "message-key", originMessage.Key, "error-queue-reason", errorQueueReason)
		}
		slog.Info("published message to error queue", "message-key", originMessage.Key, "error-queue-reason", errorQueueReason)
	}
}
