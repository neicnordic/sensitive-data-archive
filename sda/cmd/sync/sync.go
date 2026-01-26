// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	key           *[32]byte
	db            *database.SDAdb
	conf          *config.Config
	mqBroker      *broker.AMQPBroker
	archiveReader storage.Reader
	syncWriter    storage.Writer
	message       schema.DatasetMapping
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error
	conf, err = config.NewConfig("sync")
	if err != nil {
		return fmt.Errorf("failed to load config, due to: %v", err)
	}

	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer db.Close()
	if db.Version < 23 {
		return errors.New("database schema v23 is required")
	}

	mqBroker, err = broker.NewMQ(mqBroker.Conf)
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer func() {
		if err := mqBroker.Channel.Close(); err != nil {
			log.Errorf("failed to close mq broker channel due to: %v", err)
		}
		if err := mqBroker.Connection.Close(); err != nil {
			log.Errorf("failed to close mq broker connection due to: %v", err)
		}
	}()

	lb, err := locationbroker.NewLocationBroker(db)
	if err != nil {
		return fmt.Errorf("failed to initialize location broker, due to: %v", err)
	}
	syncWriter, err = storage.NewWriter(ctx, "sync", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize sync writer, due to: %v", err)
	}
	archiveReader, err = storage.NewReader(ctx, "archive")
	if err != nil {
		return fmt.Errorf("failed to initialize archive reader, due to: %v", err)
	}

	key, err = config.GetC4GHKey()
	if err != nil {
		return fmt.Errorf("failed to get c4gh key from config, due to: %v", err)
	}

	log.Info("Starting sync service")

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- startConsumer()
	}()

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
	case err := <-mqBroker.Connection.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-mqBroker.Channel.NotifyClose(make(chan *amqp.Error)):
		return err
	case err := <-consumeErr:
		return err
	}

	return nil
}
func startConsumer() error {
	messages, err := mqBroker.GetMessages(mqBroker.Conf.Queue)
	if err != nil {
		return err
	}
	for delivered := range messages {
		ctx := context.Background()
		log.Debugf("Received a message (correlation-id: %s, message: %s)",
			delivered.CorrelationId,
			delivered.Body)

		err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", mqBroker.Conf.SchemasPath), delivered.Body)
		if err != nil {
			log.Errorf("validation of incoming message (dataset-mapping) failed, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
			// Send the message to an error queue so it can be analyzed.
			infoErrorMessage := broker.InfoError{
				Error:           "Message validation failed in sync service",
				Reason:          err.Error(),
				OriginalMessage: string(delivered.Body),
			}

			body, _ := json.Marshal(infoErrorMessage)
			if err := mqBroker.SendMessage(delivered.CorrelationId, mqBroker.Conf.Exchange, "error", body); err != nil {
				log.Errorf("failed to publish message, reason: (%v)", err)
			}
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue
		}

		// we unmarshal the message in the validation step so this is safe to do
		_ = json.Unmarshal(delivered.Body, &message)

		if !strings.HasPrefix(message.DatasetID, conf.Sync.CenterPrefix) {
			log.Infoln("external dataset")
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}

			continue
		}

		var syncFilesErr error
		for _, aID := range message.AccessionIDs {
			if err := syncFiles(ctx, aID); err != nil {
				log.Errorf("failed to sync archived file: accession-id: %s, reason: (%s)", aID, err.Error())
				syncFilesErr = err

				break
			}
		}
		if syncFilesErr != nil {
			if err := delivered.Nack(false, false); err != nil {
				log.Errorf("failed to nack following GetFileSize error message")
			}

			continue
		}

		log.Infoln("buildSyncDatasetJSON")
		blob, err := buildSyncDatasetJSON(delivered.Body)
		if err != nil {
			log.Errorf("failed to build SyncDatasetJSON, Reason: %v", err)
		}
		if err := sendPOST(blob); err != nil {
			log.Errorf("failed to send POST, Reason: %v", err)
			if err := delivered.Nack(false, false); err != nil {
				log.Errorf("failed to nack following sendPOST error message")
			}

			continue
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: (%s)", err.Error())
		}
	}

	return nil
}

func syncFiles(ctx context.Context, stableID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	log.Debugf("syncing file %s", stableID)
	inboxPath, err := db.GetInboxPath(stableID)
	if err != nil {
		return fmt.Errorf("failed to get inbox path, reason: %v", err)
	}

	archivePath, archiveLocation, err := db.GetArchivePathAndLocation(stableID)
	if err != nil {
		return fmt.Errorf("failed to get archive path and location, reason: %v", err)
	}

	fileSize, err := archiveReader.GetFileSize(ctx, archiveLocation, archivePath)
	if err != nil {
		return fmt.Errorf("failed to get file size from archive storage, location: %s, path: %s, reason: %v", archiveLocation, archivePath, err)
	}

	file, err := archiveReader.NewFileReader(ctx, archiveLocation, archivePath)
	if err != nil {
		return fmt.Errorf("failed to read file from archive storage, location: %s, path: %s, reason: %v", archiveLocation, archivePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := db.GetHeaderForStableID(stableID)
	if err != nil {
		return fmt.Errorf("failed to get header from db, reason: %v", err)
	}

	newHeader, err := headers.ReEncryptHeader(header, *key, [][chacha20poly1305.KeySize]byte{*conf.Sync.PublicKey})
	if err != nil {
		return fmt.Errorf("failed to reencrypt header, reason: %v", err)
	}

	contentReader, contentWriter := io.Pipe()
	// Ensure contentReader, contentWriter are closed in case error
	defer func() {
		_ = contentWriter.Close()
		_ = contentReader.Close()
	}()

	if _, err := contentWriter.Write(newHeader); err != nil {
		return fmt.Errorf("failed to write header, reason: %v", err)
	}

	// Copy the file and check is sizes match
	copiedSize, err := io.Copy(contentWriter, file)
	switch {
	case err != nil:
		return fmt.Errorf("failed to write file content, reason: %v", err)
	case copiedSize != fileSize:
		return errors.New("copied size does not match file size")
	default:
	}
	_ = contentWriter.Close()

	_, err = syncWriter.WriteFile(ctx, inboxPath, contentReader)
	if err != nil {
		return fmt.Errorf("failed to upload file to storage, reason: %v", err)
	}
	_ = contentReader.Close()

	return nil
}

func buildSyncDatasetJSON(b []byte) ([]byte, error) {
	var msg schema.DatasetMapping
	_ = json.Unmarshal(b, &msg)

	var dataset = schema.SyncDataset{
		DatasetID: msg.DatasetID,
	}

	for _, ID := range msg.AccessionIDs {
		data, err := db.GetSyncData(ID)
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

func sendPOST(payload []byte) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	uri, err := createHostURL(conf.Sync.RemoteHost, conf.Sync.RemotePort)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(conf.Sync.RemoteUser, conf.Sync.RemotePassword)
	resp, err := client.Do(req) // #nosec G704 host originates from configuration
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	defer resp.Body.Close()

	return nil
}

func createHostURL(host string, port int) (string, error) {
	uri, err := url.ParseRequestURI(host)
	if err != nil {
		return "", err
	}
	if uri.Port() == "" && port != 0 {
		uri.Host += fmt.Sprintf(":%d", port)
	}
	uri.Path = "/dataset"

	return uri.String(), nil
}
