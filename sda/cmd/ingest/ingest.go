// The ingest service accepts messages for files uploaded to the inbox,
// registers the files in the database with their headers, and stores them
// header-stripped in the archive storage.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	ingestConf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
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
}

const headerPeekSize = 50 * 1024 * 1024

type IngestSummary struct {
	Location string
	Checksum string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := Ingest{}
	conf, err := config.NewConfig("ingest")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}

	app.MQ, err = rabbitmq.NewRabbitMQBroker(ctx)
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

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.MQ.Subscribe(ctx, "", ingestConf.SourceQueue(), app.handleMessage)
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	select {
	case <-sigc:
		slog.Info("Shutting down")
		cancel()
		<-consumeErr
	case err := <-consumeErr:
		return err
	}
	return nil
}

func (app *Ingest) handleMessage(ctx context.Context, message *v2.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Debugf("received message: %s", message.Key)

	err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", ingestConf.SchemaPath()), message.Body)
	if err != nil {
		app.publishErrorMessage(ctx, message, err)
		return nil, err
	}

	var ingestionTrigger schema.IngestionTrigger
	err = json.Unmarshal(message.Body, &ingestionTrigger)
	if err != nil {
		log.Errorf("unmarshal error: %v", err)
		return nil, nil
	}

	log.Infof("Received work (correlation-id: %s, filepath: %s, user: %s)", message.Key, ingestionTrigger.FilePath, ingestionTrigger.User)

	switch ingestionTrigger.Type {
	case "cancel":
		return app.cancelFile(ctx, message.Key, message)
	case "ingest":
		return app.ingestFile(ctx, message.Key, ingestionTrigger.FilePath, ingestionTrigger.User, ingestConf.ArchivedQueue(), message)
	default:
		log.Warnf("unknown ingest type: %s", ingestionTrigger.Type)
	}

	callbackFunc := func() {
		log.Infof("message processed and Acked: %s", message.Key)
	}

	return []func(){callbackFunc}, nil
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
	datasetID, err := app.DB.IsFileInDataset(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to query db: %w", err)
	}

	if datasetID != "" {
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
			return nil, err
		}
	}

	if app.BackupWriter != nil && archiveData.BackupFilePath != "" && archiveData.BackupLocation != "" {
		if err := app.BackupWriter.RemoveFile(ctx, archiveData.BackupLocation, archiveData.BackupFilePath); err != nil {
			log.Warnf("failed to remove file with id %s from backup due to %v", fileID, err)

			return nil, nil
		}
	}

	if err := app.DB.CancelFile(ctx, fileID, string(message.Body)); err != nil {
		return nil, err
	}

	cb := func() {
		log.Infof("successfully canceled and cleaned up file: %s", fileID)
	}

	return []func(){cb}, nil
}

func (app *Ingest) ingestFile(ctx context.Context, fileID, filePath, user, archivedQueue string, message *v2.Message) ([]func(), error) {
	status, submissionLocation, err := app.ensureFileRegistered(ctx, fileID, filePath, user)
	if err != nil {
		return nil, err
	}

	if status == "disabled" {
		return nil, nil
	}

	sourceReader, err := app.InboxReader.NewFileReader(ctx, submissionLocation, helper.UnanonymizeFilepath(filePath, user))
	if err != nil {
		if errors.Is(err, storageerrors.ErrorFileNotFoundInLocation) {
			return nil, rabbitmq.TerminalError{Err: err}
		}
		return nil, err
	}
	defer sourceReader.Close()

	summary, err := app.processAndUpload(ctx, fileID, sourceReader)
	if err != nil {
		return nil, err
	}

	if err := app.finalizeDatabaseRecords(ctx, fileID, summary, message); err != nil {
		return nil, err
	}

	callback := func() {
		err := app.notifyArchived(fileID, filePath, user, summary.Checksum, archivedQueue, message)
		if err != nil {
			log.Errorf("deferred notification failed for %s: %v", fileID, err)
		}
	}

	return []func(){callback}, nil
}

func (app *Ingest) ensureFileRegistered(ctx context.Context, fileID, filePath, user string) (string, string, error) {
	status, err := app.DB.GetFileStatus(fileID)
	if err != nil {
		return status, "", err
	}

	submissionLocation, err := app.DB.GetSubmissionLocation(ctx, fileID)
	if err != nil {
		return status, "", err
	}

	if status != "" && submissionLocation == "" {
		return status, "", fmt.Errorf("file %s is registered but missing submission location", fileID)
	}

	return status, submissionLocation, nil
}

func (app *Ingest) processAndUpload(ctx context.Context, fileID string, source io.ReadCloser) (IngestSummary, error) {
	hash := sha256.New()

	teedReader := io.TeeReader(source, hash)
	bufReader := bufio.NewReaderSize(teedReader, headerPeekSize)
	header, privateKey, err := app.decryptHeader(fileID, bufReader)
	if err != nil {
		return IngestSummary{}, rabbitmq.TerminalError{Err: fmt.Errorf("failed to decrypt file: %s", fileID)}
	}

	publicKey := keys.DerivePublicKey(*privateKey)
	keyHash := hex.EncodeToString(publicKey[:])
	if err := app.DB.SetKeyHash(keyHash, fileID); err != nil {
		return IngestSummary{}, err //TODO: Think about how to handle this, remember to implement uploadCancel logic
	}

	if err := app.DB.StoreHeader(header, fileID); err != nil {
		return IngestSummary{}, err //TODO: Think about how to handle this, remember to implement uploadCancel logic
	}

	location, err := app.ArchiveWriter.WriteFile(ctx, fileID, bufReader)
	if err != nil {
		return IngestSummary{}, err
	}

	return IngestSummary{
		Location: location,
		Checksum: fmt.Sprintf("%x", hash.Sum(nil)),
	}, nil
}

func (app *Ingest) decryptHeader(fileID string, reader *bufio.Reader) ([]byte, *[32]byte, error) {
	headerBuffer, err := reader.Peek(headerPeekSize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to peek header: %w", err)
	}

	var decryptedHeader []byte
	var matchingKey *[32]byte

	for _, key := range app.ArchiveKeyList {
		header, err := app.tryDecrypt(key, headerBuffer)
		if err == nil {
			decryptedHeader = header
			matchingKey = key
			break
		}
	}

	if matchingKey == nil {
		return nil, nil, fmt.Errorf("all keys failed to decrypt file: %s", fileID)
	}

	return decryptedHeader, matchingKey, nil
}

func (app *Ingest) finalizeDatabaseRecords(ctx context.Context, fileID string, summary IngestSummary, message *v2.Message) error {

	fileSize, err := app.ArchiveReader.GetFileSize(ctx, summary.Location, fileID)
	if err != nil {
		return err
	}

	fileInfo := database.FileInfo{}
	fileInfo.Size = fileSize
	fileInfo.Path = fileID
	fileInfo.UploadedChecksum = summary.Checksum

	status, err := app.DB.GetFileStatus(fileID)
	if err != nil {
		return err
	}

	if status == "disabled" {
		return nil
	}

	if err := app.DB.SetArchived(summary.Location, fileInfo, fileID); err != nil {
		return fmt.Errorf("Failed to mark file as archived, file-id: %s, reason: %v", fileID, err)
	}

	if err := app.DB.UpdateFileEventLog(fileID, "archived", "ingest", "{}", string(message.Body)); err != nil {
		return fmt.Errorf("Failed to update file event log, file-id: %s reason: %v", fileID, err)
	}

	return nil
}

func (app *Ingest) notifyArchived(fileID, filePath, user, checksum, archivedQueue string, message *v2.Message) error {
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

	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", ingestConf.SchemaPath()), messageBody)
	if err != nil {
		app.publishErrorMessage(context.TODO(), message, err)
		return rabbitmq.TerminalError{Err: fmt.Errorf("invalid JSON schema :%w", err)}
	}

	pubCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return app.MQ.Publish(pubCtx, archivedQueue, archivedMessage)
}

func (app *Ingest) publishErrorMessage(ctx context.Context, message *v2.Message, err error) {
	infoErrorMessage := v2.Message{
		Key:  message.Key,
		Body: []byte(fmt.Sprintf("JSON validation failed, reason: %v", err)),
	}
	if err := app.MQ.Publish(ctx, "error", infoErrorMessage); err != nil {
		log.Errorf("failed to publish message, reason: %v", err)
	}
}

func (app *Ingest) tryDecrypt(key *[32]byte, buf []byte) ([]byte, error) {
	log.Debugln("Try decrypting the first data block")
	a := bytes.NewReader(buf)
	b, err := streaming.NewCrypt4GHReader(a, *key, nil)
	if err != nil {
		log.Error(err)

		return nil, err
	}
	_, err = b.ReadByte()
	if err != nil {
		log.Error(err)

		return nil, err
	}

	f := bytes.NewReader(buf)
	header, err := headers.ReadHeader(f)
	if err != nil {
		log.Error(err)

		return nil, err
	}

	return header, nil
}
