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
	ingestconf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	v2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2" //nolint: revive
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
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
	DB             *database.SDAdb
	InboxReader    storage.Reader
	MQ             v2.Broker
	SchemaPath     string
	SourceQueue    string
	ArchivedQueue  string
}

type DecryptResult struct {
	keyHash  string
	checksum string
	header   []byte
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
		return fmt.Errorf("failed to load config, due to: %v", err)
	}

	app := Ingest{
		SchemaPath:    ingestconf.SchemaPath(),
		SourceQueue:   ingestconf.SourceQueue(),
		ArchivedQueue: ingestconf.ArchivedQueue(),
	}

	conf, err := config.NewConfig("ingest")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}

	app.MQ, err = rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}

	defer func() {
		if app.MQ == nil {
			return
		}
		if err := app.MQ.Close(); err != nil {
			log.Errorf("could not close MQ, due to: %v", err)
		}
	}()

	app.DB, err = database.NewSDAdb(conf.Database)
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
		consumeErr <- app.MQ.Subscribe(ctx, app.SourceQueue, app.handleMessage)
	}()

	select {
	case sig := <-sigc:
		log.Infof("recieved signal: %v, shutting down gracefully", sig)
		cancel()

		return nil
	case err := <-consumeErr:
		if err != nil && err != context.Canceled {
			log.Errorf("failed to consume from %s, due to: %v", app.SourceQueue, err)
			cancel()

			return err
		}

		return nil
	}
}

func (app *Ingest) handleMessage(ctx context.Context, message *v2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Debugf("received message: %s", message.Key)

	err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", app.SchemaPath), message.Body)
	if err != nil {
		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}

	var ingestionTrigger schema.IngestionTrigger
	err = json.Unmarshal(message.Body, &ingestionTrigger)
	if err != nil {
		log.Errorf("could not unmarshall message, due to: %v", err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}
	log.Infof("received work (correlation-id: %s, filepath: %s, user: %s)", message.Key, ingestionTrigger.FilePath, ingestionTrigger.User)

	var callbacks []func()
	switch ingestionTrigger.Type {
	case "cancel":
		callbacks, err = app.cancelFile(ctx, message.Key, message)
	case "ingest":
		callbacks, err = app.ingestFile(ctx, message.Key, ingestionTrigger.FilePath, ingestionTrigger.User, app.ArchivedQueue, message)
	default:
		log.Warnf("unknown ingest type: %s", ingestionTrigger.Type)

		return nil, errors.New("unknonw ingest type")
	}

	return callbacks, err
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

func (app *Ingest) cancelFile(ctx context.Context, fileID string, message *v2.Message) ([]func(), error) {
	fileExistsInDataset, err := app.DB.IsFileInDataset(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to query db: %v", err)
	}

	if fileExistsInDataset {
		log.Warnf("cannot cancel file: %s, as it has been added to a dataset", fileID)

		return nil, nil
	}

	archiveData, err := app.DB.GetArchived(fileID)
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

			return nil, err
		}
	}

	if app.BackupWriter != nil && archiveData.BackupFilePath != "" && archiveData.BackupLocation != "" {
		if err := app.BackupWriter.RemoveFile(ctx, archiveData.BackupLocation, archiveData.BackupFilePath); err != nil {
			log.Errorf("failed to remove file with id %s from backup due to %v", fileID, err)

			return nil, err
		}
	}

	if err := app.DB.CancelFile(ctx, fileID, string(message.Body)); err != nil {
		return nil, err
	}

	log.Infof("successfully canceled and cleaned up file: %s", fileID)

	return nil, nil
}

func (app *Ingest) ingestFile(ctx context.Context, fileID, filePath, user, archivedQueue string, message *v2.Message) ([]func(), error) {
	status, err := app.DB.GetFileStatus(fileID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Errorf("could not get file status for file: %s, due to %v", fileID, err)

		return nil, err
	}

	submissionLocation, err := app.DB.GetSubmissionLocation(ctx, fileID)
	if err != nil {
		log.Errorf("failed to find submission location for file in all configured storage locations, file-id: %s", fileID)

		return nil, err
	}

	switch status {
	case "uploaded", "disabled":

	case "":
		var findFileErr, registerErr error
		location, findFileErr := app.InboxReader.FindFile(ctx, message.Key)
		fileID, registerErr = app.DB.RegisterFile(&fileID, location, message.Key, user)
		if registerErr != nil {
			log.Errorf("failed to register file: %s, due to: %v", fileID, registerErr)

			return nil, registerErr
		}

		if findFileErr != nil {
			log.Errorf("failed to find submission location for file: %s, due to: %v", fileID, findFileErr)

			return []func(){app.errorQueue(message), app.setErrorEvent(findFileErr.Error(), message)}, findFileErr
		}

		return nil, nil

	default:
		log.Warnf("file: %s recieved ingestion trigger with status: %s", fileID, status)

		return nil, fmt.Errorf("cannot ingest file with status: %s", status)
	}

	sourceReader, err := app.InboxReader.NewFileReader(ctx, submissionLocation, helper.UnanonymizeFilepath(filePath, user))
	if err != nil {
		log.Errorf("failed to read file, due to: %v", err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}
	defer sourceReader.Close()

	if err := app.DB.UpdateFileEventLog(fileID, "submitted", "ingest", "{}", string(message.Body)); err != nil {
		log.Errorf("failed to set ingestion status for file from message, file-id: %s, due to: %v", fileID, err)
	}

	decryptResult, err := app.decrypt(sourceReader)
	if err != nil {
		log.Errorf("failed ingestion during decrypt and archive for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}

	location, err := app.archive(ctx, decryptResult.keyHash, fileID, decryptResult.header, sourceReader)
	if err != nil {
		log.Errorf("failed to archive file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}

	if err := app.finalizeDatabaseRecords(ctx, fileID, location, decryptResult.checksum, message); err != nil {
		log.Errorf("failed to finalize databse records for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}

	if err := app.notifyArchived(ctx, fileID, filePath, user, decryptResult.checksum, archivedQueue, message); err != nil {
		log.Errorf("failed to send to archived message for file: %s, due to: %v", fileID, err)

		return []func(){app.errorQueue(message), app.setErrorEvent(err.Error(), message)}, err
	}

	log.Infof("file %s ingested sucessfully", fileID)

	return nil, nil
}

func (app *Ingest) decrypt(source io.ReadCloser) (DecryptResult, error) {
	hash := sha256.New()
	teedReader := io.TeeReader(source, hash)
	var headerBuf bytes.Buffer
	headerTee := io.TeeReader(teedReader, &headerBuf)

	header, err := headers.ReadHeader(headerTee)
	if err != nil {
		return DecryptResult{}, fmt.Errorf("failed to parse crypt4gh header, due to: %v", err)
	}

	var validKey *[32]byte
	for _, key := range app.ArchiveKeyList {
		if _, err := streaming.NewCrypt4GHReader(bytes.NewReader(header), *key, nil); err == nil {
			validKey = key

			break
		}
	}

	if validKey == nil {
		return DecryptResult{}, errors.New("no valid keys found to decrypt file")
	}

	publicKey := keys.DerivePublicKey(*validKey)
	keyHash := hex.EncodeToString(publicKey[:])
	checksum := fmt.Sprintf("%x", hash.Sum(nil))

	return DecryptResult{keyHash: keyHash, checksum: checksum, header: header}, err
}

func (app *Ingest) archive(ctx context.Context, keyHash, fileID string, rawHeader []byte, reader io.Reader) (string, error) {
	if err := app.DB.SetKeyHash(keyHash, fileID); err != nil {
		return "", err
	}

	// TODO: Remember to clean up previous DB call in case next one errors (eg use transactions)
	if err := app.DB.StoreHeader(rawHeader, fileID); err != nil {
		return "", err
	}

	location, err := app.ArchiveWriter.WriteFile(ctx, fileID, reader)
	if err != nil {
		return "", err
	}

	return location, nil
}

func (app *Ingest) finalizeDatabaseRecords(ctx context.Context, fileID, location, checksum string, message *v2.Message) error {
	fileSize, err := app.ArchiveReader.GetFileSize(ctx, location, fileID)
	if err != nil {
		return err
	}

	fileInfo := database.FileInfo{}
	fileInfo.Size = fileSize
	fileInfo.Path = fileID
	fileInfo.UploadedChecksum = checksum

	status, err := app.DB.GetFileStatus(fileID)
	if err != nil {
		return err
	}

	if status == "disabled" {
		return nil
	}

	if err := app.DB.SetArchived(location, fileInfo, fileID); err != nil {
		return fmt.Errorf("failed to mark file as archived, file-id: %s, due to: %v", fileID, err)
	}

	if err := app.DB.UpdateFileEventLog(fileID, "archived", "ingest", "{}", string(message.Body)); err != nil {
		return fmt.Errorf("failed to update file event log, file-id: %s due to: %v", fileID, err)
	}

	return nil
}

func (app *Ingest) notifyArchived(ctx context.Context, fileID, filePath, user, checksum, archivedQueue string, message *v2.Message) error {
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

	archivedMessage := v2.Message{
		Key:  message.Key,
		Body: messageBody,
	}

	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.SchemaPath), messageBody)
	if err != nil {
		return err
	}

	return app.MQ.Publish(ctx, archivedQueue, archivedMessage)
}

func (app *Ingest) setErrorEvent(details string, message *v2.Message) func() {
	return func() {
		detailsMap := map[string]string{
			"error": details,
		}

		detailsJSON, err := json.Marshal(detailsMap)
		if err != nil {
			log.Errorf("failed to marshal details to JSON, due to: %v", err)
			detailsJSON = []byte("{}")
		}
		err = app.DB.UpdateFileEventLog(message.Key, "error", "ingest", string(detailsJSON), string(message.Body))
		if err != nil {
			log.Debugf("error from database when setting error event, due to: %v", err)
		}
	}
}

func (app *Ingest) errorQueue(message *v2.Message) func() {
	return func() {
		if err := app.MQ.Publish(context.Background(), "error", *message); err != nil {
			log.Errorf("failed to publish to error queue: %v", err)
		}
		log.Info("published message to error queue", "message", message.Key)
	}
}
