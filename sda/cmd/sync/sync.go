// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var (
	archive, syncInbox storage.Backend
	err  error
	db   *database.SDAdb
	conf *config.Config
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
	archive, err = storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatalf("archive error: %s",err.Error())
	}
	syncInbox, err = storage.NewBackend(conf.Sync.Destination)
	if err != nil {
		log.Fatalf("destination error: %s",err.Error())
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

	go func() {
		messages, err := mq.GetMessages("sync_files")
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
			delivered.CorrelationId,
			delivered.Body)
			var msgType struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(delivered.Body, &msgType)
	
			switch msgType.Type {
			case "sync":
				var message schema.SyncFileData
				_ = json.Unmarshal(delivered.Body, &message)
				if err := syncFile(message); err != nil {
					log.Errorf("failed to sync archived file %s, reason: (%s)", message.AccessionID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetFileSize error message")
					}

					continue
				}

				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}
			case "sync-archive":
				var message schema.SyncArchiveFile
				_ = json.Unmarshal(delivered.Body, &message)
				if err := syncArchiveFile(message); err != nil {
					log.Errorf("failed to sync archived file %s, reason: (%s)", message.FileID, err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetFileSize error message")
					}

					continue
				}

				log.Infoln("trigger verify")

				log.Debugln("move sync to archive completed")
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}
			}
		}
	}()

	<-forever
}

func syncArchiveFile(message schema.SyncArchiveFile) error {
	log.Debugf("syncing file: %s", message.SyncInboxPath)
	file, err := syncInbox.NewFileReader(message.SyncInboxPath)
	if err != nil {
		log.Debugln("failed to open file in sync bucket")

		return err
	}
	defer file.Close()

	dest, err :=  archive.NewFileWriter(message.FileID)
	if err != nil {
		log.Debugln("failed to open file for writing in archive")

		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, file)
	if err != nil {
		log.Errorf("failed to copy file, reason: %v)", err)

		return err
	}

	return nil
}

func syncFile(message schema.SyncFileData) error {
	log.Debugf("syncing file: %s", message.AccessionID)

	header, err := db.GetHeaderForStableID(message.AccessionID)
	if err != nil {
		return fmt.Errorf("failed to get header for %s, (%s)", message.AccessionID, err.Error())
	}

	newHeader, err := reencryptHeader(conf.Sync.Reencrypt, header, conf.Sync.PublicKey)
	if err != nil {
		return err
	}

	syncMsg, _ := json.Marshal(schema.SyncFileData{
		AccessionID:       message.AccessionID,
		ArchivePath:       message.ArchivePath,
		CorrelationID:     message.CorrelationID,
		DecryptedChecksum: message.DecryptedChecksum,
		FilePath:          message.FilePath,
		Header:            hex.EncodeToString(newHeader),
		Type:              "sync",
		User:              message.User,
	})
	if err := schema.ValidateJSON(fmt.Sprintf("%s/sync-file.json", conf.Broker.SchemasPath), syncMsg); err != nil {
		return err
	}

	if err := sendPOST(syncMsg, "file"); err != nil {
		return err
	}

	return nil
}

func sendPOST(payload []byte, route string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	url, err := url.ParseRequestURI(conf.Sync.RemoteHost)
	if err != nil {
		return err
	}
	if url.Port() == "" && conf.Sync.RemotePort != 0 {
		url.Host += fmt.Sprintf(":%d", conf.Sync.RemotePort)
	}
	url.Path = route

	req, err := http.NewRequest(http.MethodPost, url.String(), bytes.NewBuffer(payload))
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

func reencryptHeader(conf config.ReencryptConfig, oldHeader []byte, reencKey string) ([]byte, error) {
	opts, err := config.TLSReencryptConfig(conf)
	if err != nil {
		log.Errorf("Failed to create reencrypt options, reason: %s", err)

		return nil, err
	}
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%d", conf.Host, conf.Port),
		grpc.WithTransportCredentials(opts),
	)
	if err != nil {
		log.Errorf("Failed to connect to the reencrypt service, reason: %s", err)

		return nil, err
	}
	defer conn.Close()

	timeoutDuration := 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	c := reencrypt.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{Oldheader: oldHeader, Publickey: reencKey})
	if err != nil {
		log.Errorf("Failed response from the reencrypt service, reason: %s", err)

		return nil, err
	}
	log.Debugf("Response from the reencrypt service: %v", res)

	return res.Header, nil
}
