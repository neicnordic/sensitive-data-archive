// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	err                      error
	key, publicKey           *[32]byte
	db                       *database.SDAdb
	conf                     *config.Config
	archive, syncDestination storage.Backend
)

func main() {
	forever := make(chan bool)
	conf, err = config.NewConfig("sync")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}

	syncDestination, err = storage.NewBackend(conf.Sync.Destination)
	if err != nil {
		log.Fatal(err)
	}
	archive, err = storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}

	key, err = config.GetC4GHKey()
	if err != nil {
		log.Fatal(err)
	}

	publicKey, err = config.GetC4GHPublicKey("sync")
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()
	defer db.Close()

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := mq.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("Starting sync service")
	var message schema.DatasetMapping

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (dataset-mapping) failed, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed in sync service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
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

			for _, aID := range message.AccessionIDs {
				if err := syncFiles(aID); err != nil {
					log.Errorf("failed to sync archived file: accession-id: %s, reason: (%s)", aID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetFileSize error message")
					}

					continue
				}
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
	}()

	<-forever
}

func syncFiles(stableID string) error {
	log.Debugf("syncing file %s", stableID)
	inboxPath, err := db.GetInboxPath(stableID)
	if err != nil {
		return fmt.Errorf("failed to get inbox path for file with stable ID: %s, reason: %v", stableID, err)
	}

	archivePath, err := db.GetArchivePath(stableID)
	if err != nil {
		return fmt.Errorf("failed to get archive path for file with stable ID: %s, reason: %v", stableID, err)
	}

	fileSize, err := archive.GetFileSize(archivePath, false)
	if err != nil {
		return fmt.Errorf("failed to get filesize for file with archivepath: %s, reason: %v", archivePath, err)
	}

	file, err := archive.NewFileReader(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	dest, err := syncDestination.NewFileWriter(inboxPath)
	if err != nil {
		return err
	}
	defer dest.Close()

	header, err := db.GetHeaderForStableID(stableID)
	if err != nil {
		return err
	}

	pubkeyList := [][chacha20poly1305.KeySize]byte{}
	pubkeyList = append(pubkeyList, *publicKey)
	newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
	if err != nil {
		return err
	}

	_, err = dest.Write(newHeader)
	if err != nil {
		return err
	}

	// Copy the file and check is sizes match
	copiedSize, err := io.Copy(dest, file)
	if err != nil || copiedSize != int64(fileSize) {
		switch {
		case copiedSize != int64(fileSize):
			return errors.New("copied size does not match file size")
		default:
			return err
		}
	}

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
	resp, err := client.Do(req)
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
