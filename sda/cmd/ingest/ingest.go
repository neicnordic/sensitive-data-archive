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
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
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
	InboxConfig    helper.InboxConfig
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
	app.InboxConfig = config.LoadInboxConfig()

	app.Broker, err = rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}

	defer func() {
		if app.Broker == nil {
			return
		}
		if err := app.Broker.Close(); err != nil {
			log.Errorf("could not close Broker, due to: %v", err)
		}
	}()

	app.db, err = postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db due to: %v", err)
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
		return fmt.Errorf("failed to register c4gh key, due to: %v", err)
	}
	storageLocationBroker, err := locationbroker.NewLocationBroker(app.db)
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
	if err != nil && !errors.Is(err, storageerrors.ErrorNoValidWriter) {
		return fmt.Errorf("failed to initialize backup writer due to: %v", err)
	}
	if backupWriter != nil {
		log.Info("backup writer initialized, will clean cancelled files from backup storage")
		app.BackupWriter = backupWriter
	} else {
		log.Info("no backup writer initialized, will NOT clean cancelled files from backup storage")
	}
	log.Info("starting ingest service")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.Broker.Subscribe(ctx, ingestconf.SourceQueue(), app.handleMessage)
	}()

	select {
	case sig := <-sigc:
		log.Infof("recieved signal: %v, shutting down gracefully", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			log.Errorf("failed to consume from %s, due to: %v", ingestconf.SourceQueue(), err)
			cancel()

			return err
		}

		return nil
	}
}

func (app *Ingest) handleMessage(ctx context.Context, message *brokerv2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", ingestconf.SchemaPath()), message.Body)
	if err != nil {
		log.Errorf("could not validate message: %s, due to: %v", message.Key, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	var ingestionTrigger schema.IngestionTrigger
	err = json.Unmarshal(message.Body, &ingestionTrigger)
	if err != nil {
		log.Errorf("could not unmarshall message, due to: %v", err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}
	log.Infof("received work (correlation-id: %s, filepath: %s, user: %s)", message.Key, ingestionTrigger.FilePath, ingestionTrigger.User)

	var callbacks []func()
	switch ingestionTrigger.Type {
	case "cancel":
		callbacks, err = app.cancelFile(ctx, message.Key, message)
	case "ingest":
		callbacks, err = app.ingestFile(ctx, message.Key, ingestionTrigger.FilePath, ingestionTrigger.User, ingestconf.ArchivedQueue(), message)
	default:
		log.Warnf("unknown ingest type: %s", ingestionTrigger.Type)

		return nil, errors.New("unknonw ingest type")
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
		return nil, fmt.Errorf("failed to query db: %v", err)
	}

	if fileExistsInDataset {
		log.Warnf("cannot cancel file: %s, as it has been added to a dataset", fileID)

		return []func(){app.errorQueue(message), app.setErrorEvent("cannot cancel file: already added to a dataset", message)}, nil
	}

	archiveData, err := app.db.GetArchived(ctx, fileID)
	if err != nil {
		return nil, err
	}

	if archiveData == nil {
		log.Warnf("file %s not found in archive, skipping", fileID)

		return nil, nil
	}

	if archiveData.Location != "" {
		if err := app.ArchiveWriter.RemoveFile(ctx, archiveData.Location, archiveData.FilePath); err != nil {
			log.Errorf("failed to remove file with id %s from backup due to %v", fileID, err)

			return nil, nil
		}
	}

	if app.BackupWriter != nil && archiveData.BackupFilePath != "" && archiveData.BackupLocation != "" {
		if err := app.BackupWriter.RemoveFile(ctx, archiveData.BackupLocation, archiveData.BackupFilePath); err != nil {
			log.Errorf("failed to remove file with id %s from backup due to %v", fileID, err)

			return nil, nil
		}
	}

	// Ideally this transaction should span the whole message processing, but for now just spans the CancelFile
	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		log.Errorf("failed to begin transaction, reason: %v", err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}
	if err := tx.CancelFile(ctx, fileID, string(message.Body)); err != nil {
		log.Errorf("failed to cancel file with id: %s, due to %v", fileID, err)

		if err := tx.Rollback(); err != nil {
			log.Errorf("failed to rollback CancelFile transaction, reason: %v", err)
		}

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit CancelFile transaction, reason: %v", err)
		_ = tx.Rollback()

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	log.Infof("successfully canceled and cleaned up file: %s", fileID)

	return nil, nil
}

func (app *Ingest) ingestFile(ctx context.Context, fileID, filePath, user, archivedQueue string, message *brokerv2.Message) ([]func(), error) {
	status, err := app.db.GetFileStatus(ctx, fileID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Errorf("could not get file status for file: %s, due to %v", fileID, err)

		return nil, nil
	}

	submissionLocation, err := app.db.GetSubmissionLocation(ctx, fileID)
	if err != nil {
		log.Errorf("failed to get submission location for file: %s, due to :%v", fileID, err)

		return nil, nil
	}

	switch status {
	case "uploaded", "disabled":

	case "":
		// Catch all for implementations inbox uploading that does not register the file in the DB, e.g. for those not using S3inbox or sftpInbox
		// Since we dont have the submission location in storage, we need to look through all configured storage locations.
		var findFileErr, registerErr error
		// Resolve the anonymized submission path to its physical inbox path before locating it.
		submissionLocation, findFileErr = app.InboxReader.FindFile(ctx, helper.ResolveInboxPath(message.Key, user, app.InboxConfig))

		// Ideally this transaction should span the whole message processing, but for now just spans the RegisterFile
		tx, err := app.db.BeginTransaction(ctx)
		if err != nil {
			log.Errorf("failed to begin transaction, reason: %v", err)

			return nil, nil
		}

		// Register file even if FindFile didnt succeed with submissionLocation == "", as we will add an error file event log to it in that case
		fileID, registerErr = tx.RegisterFile(ctx, &fileID, submissionLocation, message.Key, user)
		if registerErr != nil {
			log.Errorf("failed to register file, fileID: %s, reason: (%s)", fileID, registerErr.Error())
			if err := tx.Rollback(); err != nil {
				log.Errorf("failed to rollback RegisterFile transaction, reason: %v", err)
			}

			return nil, nil
		}
		if err := tx.Commit(); err != nil {
			log.Errorf("failed to commit RegisterFile transaction, reason: %v", err)
			_ = tx.Rollback()
		}

		if findFileErr != nil {
			log.Errorf("failed to find submission location for file: %s, due to: %v", fileID, findFileErr)

			return []func(){app.errorQueue(message), app.setErrorEvent(findFileErr.Error(), message)}, nil
		}

		return nil, nil

	default:
		log.Warnf("file: %s recieved ingestion trigger with status: %s", fileID, status)

		return nil, fmt.Errorf("cannot ingest file with status: %s", status)
	}

	sourceReader, err := app.InboxReader.NewFileReader(ctx, submissionLocation, helper.ResolveInboxPath(filePath, user, app.InboxConfig))
	if err != nil {
		log.Errorf("failed to read file, due to: %v", err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}
	defer sourceReader.Close()

	if err := app.db.UpdateFileEventLog(ctx, fileID, "submitted", "ingest", "{}", string(message.Body)); err != nil {
		log.Errorf("failed to set ingestion status for file from message, file-id: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message)}, nil
	}

	decryptResult, err := app.decrypt(sourceReader)
	if err != nil {
		log.Errorf("failed ingestion during decrypt and archive for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	location, err := app.archive(ctx, decryptResult.keyHash, fileID, decryptResult.header, decryptResult.teedReader)
	if err != nil {
		log.Errorf("failed to archive file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	checksum := fmt.Sprintf("%x", decryptResult.hash.Sum(nil))

	if err := app.finalizeDatabaseRecords(ctx, fileID, location, checksum, message); err != nil {
		log.Errorf("failed to finalize databse records for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	if err := app.notifyArchived(ctx, fileID, filePath, user, checksum, archivedQueue, message); err != nil {
		log.Errorf("failed to send to archived message for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, nil
	}

	log.Infof("file %s ingested sucessfully", fileID)

	return nil, nil
}

func (app *Ingest) decrypt(source io.ReadCloser) (decryptResult, error) {
	fileHash := sha256.New()
	teedReader := io.TeeReader(source, fileHash)
	var headerBuf bytes.Buffer
	headerTee := io.TeeReader(teedReader, &headerBuf)

	header, err := headers.ReadHeader(headerTee)
	if err != nil {
		return decryptResult{}, fmt.Errorf("failed to parse crypt4gh header, due to: %v", err)
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

func (app *Ingest) archive(ctx context.Context, keyHash, fileID string, rawHeader []byte, reader io.Reader) (string, error) {
	if err := app.db.SetKeyHash(ctx, keyHash, fileID); err != nil {
		return "", err
	}

	// TODO: Remember to clean up previous DB call in case next one errors (eg use transactions)
	if err := app.db.StoreHeader(ctx, rawHeader, fileID); err != nil {
		return "", err
	}

	location, err := app.ArchiveWriter.WriteFile(ctx, fileID, reader)
	if err != nil {
		return "", err
	}

	return location, nil
}

func (app *Ingest) finalizeDatabaseRecords(ctx context.Context, fileID, location, checksum string, message *brokerv2.Message) error {
	log.Infof("finalizeDatabaseRecords: fileID=%s checksum=%s", fileID, checksum)
	fileSize, err := app.ArchiveReader.GetFileSize(ctx, location, fileID)
	if err != nil {
		return err
	}

	fileInfo := new(database.FileInfo)
	fileInfo.Path = fileID
	fileInfo.Size = fileSize
	fileInfo.UploadedChecksum = checksum

	status, err := app.db.GetFileStatus(ctx, fileID)
	if err != nil {
		return err
	}

	if err := app.db.SetArchived(ctx, location, fileInfo, fileID); err != nil {
		return fmt.Errorf("failed to mark file as archived, file-id: %s, due to: %v", fileID, err)
	}

	if status == "disabled" {
		return nil
	}

	if err := app.db.UpdateFileEventLog(ctx, fileID, "archived", "ingest", "{}", string(message.Body)); err != nil {
		return fmt.Errorf("failed to update file event log, file-id: %s due to: %v", fileID, err)
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
		log.Errorf("could not validate message: %s, due to: %v", message.Key, err)

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
			log.Errorf("failed to marshal details to JSON, due to: %v", err)
			detailsJSON = []byte("{}")
		}
		err = app.db.UpdateFileEventLog(context.Background(), message.Key, "error", "ingest", string(detailsJSON), string(message.Body))
		if err != nil {
			log.Errorf("error from database when setting error event, due to: %v", err)
		}
	}
}

func (app *Ingest) errorQueue(message *brokerv2.Message) func() {
	return func() {
		if err := app.Broker.Publish(context.Background(), "error", *message); err != nil {
			log.Errorf("failed to publish to error queue: %v", err)
		}
		log.Infof("published message: %s to error queue", message.Key)
	}
}
