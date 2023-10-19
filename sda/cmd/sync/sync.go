// The backup command accepts messages with accessionIDs for
// ingested files and copies them to the second storage.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("sync")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	syncDestination, err := storage.NewBackend(conf.Sync)
	if err != nil {
		log.Fatal(err)
	}
	archive, err := storage.NewBackend(conf.Archive)
	if err != nil {
		log.Fatal(err)
	}

	var key *[32]byte
	var publicKey *[32]byte
	key, err = config.GetC4GHKey()
	if err != nil {
		log.Fatal(err)
	}

	publicKey, err = config.GetC4GHPublicKey()
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
	var message schema.IngestionCompletion

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", conf.Broker.SchemasPath), delivered.Body)

			if err != nil {
				log.Errorf("Validation of incoming message failed "+
					"(corr-id: %s, error: %v)",
					delivered.CorrelationId,
					err)

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			log.Infof("Received work (corr-id: %s, "+
				"filepath: %s, "+
				"user: %s, "+
				"accessionid: %s, "+
				"decryptedChecksums: %v)",
				delivered.CorrelationId,
				message.FilePath,
				message.User,
				message.AccessionID,
				message.DecryptedChecksums)

			var filePath string
			var fileSize int
			if filePath, fileSize, err = db.GetArchived(delivered.CorrelationId); err != nil {
				log.Errorf("GetArchived failed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				// nack the message but requeue until we fixed the SQL retry.
				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of GetArchived failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			log.Debug("Backup initiated")

			// Get size on disk, will also give some time for the file to
			// appear if it has not already

			diskFileSize, err := archive.GetFileSize(filePath)

			if err != nil {
				log.Errorf("Failed to get size info for archived file %s "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of GetFileSize failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			if diskFileSize != int64(fileSize) {
				log.Errorf("File size in archive does not match database for archive file %s "+
					"- archive size is %d, database has %d "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					diskFileSize,
					fileSize,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of file size differences failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			file, err := archive.NewFileReader(filePath)
			if err != nil {
				log.Errorf("Failed to open archived file %s "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				//FIXME: should it retry?
				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of NewFileReader failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			dest, err := syncDestination.NewFileWriter(message.FilePath)
			if err != nil {
				log.Errorf("Failed to open backup file %s for writing "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				//FIXME: should it retry?
				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of NewFileWriter failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			// Get the header from db
			header, err := db.GetHeaderForStableID(message.AccessionID)
			if err != nil {
				log.Errorf("GetHeaderForStableID failed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)
			}

			// Decrypt header
			log.Debug("Decrypt header")
			DecHeader, err := FormatHexHeader(header)
			if err != nil {
				log.Errorf("Failed to decode the header %s "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of decode header failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}
			}

			// Reencrypt header
			log.Debug("Reencrypt header")
			pubkeyList := [][chacha20poly1305.KeySize]byte{}
			pubkeyList = append(pubkeyList, *publicKey)
			newHeader, err := headers.ReEncryptHeader(DecHeader, *key, pubkeyList)
			if err != nil {
				log.Errorf("Failed to reencrypt the header %s "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of reencrypt header failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}
			}

			// write header to destination file
			_, err = dest.Write(newHeader)
			if err != nil {
				log.Errorf("Failed to write the header to destination %s "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					filePath,
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)
			}


			// Copy the file and check is sizes match
			copiedSize, err := io.Copy(dest, file)
			if err != nil || copiedSize != int64(fileSize) {
				log.Errorf("Failed to copy file "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				//FIXME: should it retry?
				if e := delivered.Nack(false, true); e != nil {
					log.Errorf("Failed to NAck because of Copy failed "+
						"(corr-id: %s, "+
						"filepath: %s, "+
						"user: %s, "+
						"accessionid: %s, "+
						"decryptedChecksums: %v, error: %v)",
						delivered.CorrelationId,
						message.FilePath,
						message.User,
						message.AccessionID,
						message.DecryptedChecksums,
						e)
				}

				continue
			}

			file.Close()
			dest.Close()

			log.Infof("Backuped file %s (%d bytes) from archive to backup "+
				"(corr-id: %s, "+
				"filepath: %s, "+
				"user: %s, "+
				"accessionid: %s, "+
				"decryptedChecksums: %v)",
				filePath,
				fileSize,
				delivered.CorrelationId,
				message.FilePath,
				message.User,
				message.AccessionID,
				message.DecryptedChecksums)

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, delivered.Body); err != nil {
				// TODO fix resend mechanism
				log.Errorf("Failed to send message for completed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

				// Restart loop, do not ack
				continue
			}

			if err := delivered.Ack(false); err != nil {

				log.Errorf("Failed to ack message after work completed "+
					"(corr-id: %s, "+
					"filepath: %s, "+
					"user: %s, "+
					"accessionid: %s, "+
					"decryptedChecksums: %v, error: %v)",
					delivered.CorrelationId,
					message.FilePath,
					message.User,
					message.AccessionID,
					message.DecryptedChecksums,
					err)

			}
		}
	}()

	<-forever
}

// FormatHexHeader decodes a hex formatted file header, and returns the data as a binary
func FormatHexHeader(hexData string) ([]byte, error) {

	// Trim whitespace that might otherwise confuse the hex parse
	headerHexStr := strings.TrimSpace(hexData)

	// Decode the hex
	binaryHeader, err := hex.DecodeString(headerHexStr)
	if err != nil {
		return nil, err
	}

	return binaryHeader, nil
}