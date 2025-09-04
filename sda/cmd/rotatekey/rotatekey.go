// The rotatekey service accepts messages for files mapped to a dataset,
// re-encrypts their header with a configured public key and stores it
// in the database together with the key-hash of the rotation key.
// I then sends a message to verify so the file is re-verified.

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	err            error
	publicKey      *[32]byte
	archiveKeyList []*[32]byte
	db             *database.SDAdb
	conf           *config.Config
)

func main() {
	forever := make(chan bool)
	conf, err = config.NewConfig("rotatekey")
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

	archiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil || len(archiveKeyList) == 0 {
		log.Fatal("no C4GH private keys configured")
	}

	publicKey, err = config.GetC4GHPublicKey("rotatekey")
	if err != nil {
		log.Fatal(err)
	}

	// Check that key is registered in the db at startup
	keyhash, err := getKeyHash()
	if err != nil {
		log.Fatalf("database lookup of the rotation key failed, reason: %v", err)
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

	log.Info("Starting rotatekey service")
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
				log.Errorf("validation of incoming message (dataset-mapping) failed, reason: %v", err)
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed in rotatekey service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			// Fetch rotate key hash before starting work so that we make sure the hash state
			// has not changed since the application startup.
			keyhash, err = getKeyHash()
			if err != nil {
				log.Errorf("database lookup of the rotation key failed, reason: %v", err)
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Lookup of rotation key hash failed in rotatekey service",
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			for _, aID := range message.AccessionIDs {
				fileID, err := db.GetFileIDbyAccessionID(aID)
				if err != nil {
					log.Errorf("failed to get file-id for file with accession-id: %s, reason: %v", aID, err)

					continue
				}

				// Get current keyhash for the file, send to error queue if this fails
				oldKeyHash, err := db.GetKeyHash(fileID)
				if err != nil {
					log.Errorf("failed to get keyhash for file with accession-id: %s, reason: %v", aID, err)
					// Send the message to an error queue so it can be analyzed.
					infoErrorMessage := broker.InfoError{
						Error:           "Failed to get keyhash in rotatekey service",
						Reason:          err.Error(),
						OriginalMessage: string(delivered.Body),
					}

					body, _ := json.Marshal(infoErrorMessage)
					if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
						log.Errorf("failed to publish message, reason: (%s)", err.Error())
					}
					if err := delivered.Ack(false); err != nil {
						log.Errorf("failed to Ack message, reason: (%s)", err.Error())
					}

					continue
				}

				// Check that the file is not already encrypted with the target key
				if oldKeyHash == keyhash {
					log.Errorf("the file with file-id: %s is already encrypted with the given rotation c4gh key", fileID)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following already encrypted with key error message")
					}

					continue
				}

				newHeader, err := reencryptFileHeader(aID)
				if err != nil {
					log.Errorf("failed to rotate c4gh key for file %s, reason: %v", aID, err)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following reencryptFiles error message")
					}

					continue
				}

				// Rotate header in database
				if err := db.StoreHeader(newHeader, fileID); err != nil {
					log.Errorf("StoreHeader failed for file-id: %s, reason: %v", fileID, err)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following storeheader error message")
					}

					continue
				}

				// Rotate keyhash
				if err := db.SetKeyHash(keyhash, fileID); err != nil {
					log.Errorf("SetKeyHash failed for file-id: %s, reason: %v", fileID, err)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following setKeyHash error message")
					}

					continue
				}

				// Send re-verify message
				reVerify, err := db.GetReVerificationData(aID)
				if err != nil {
					log.Errorf("GetReVerificationData failed for file-id: %s, reason: %v", fileID, err)

					continue
				}

				reVerifyMsg, _ := json.Marshal(&reVerify)
				err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", conf.Broker.SchemasPath), reVerifyMsg)
				if err != nil {
					log.Errorf("Validation of outgoing re-verify message failed, reason: %v", err)

					continue
				}

				corrID, err := db.GetCorrID(reVerify.User, reVerify.FilePath, aID)
				if err != nil {
					log.Errorf("failed to get CorrID for %s, %s", reVerify.User, reVerify.FilePath)

					continue
				}

				if err := mq.SendMessage(corrID, conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())

					continue
				}
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}
		}
	}()

	<-forever
}

func reencryptFileHeader(stableID string) ([]byte, error) {
	log.Debugf("rotating c4gh key for file with stable-id: %s", stableID)

	header, err := db.GetHeaderForStableID(stableID)
	if err != nil {
		return nil, err
	}

	// determine decryption key
	var key *[32]byte
	for _, k := range archiveKeyList {
		size, err := headers.EncryptedSegmentSize(header, *k)
		if (err == nil) && (size != 0) {
			key = k

			break
		}
	}

	pubkeyList := [][chacha20poly1305.KeySize]byte{*publicKey}
	newHeader, err := headers.ReEncryptHeader(header, *key, pubkeyList)
	if err != nil {
		return nil, err
	}

	return newHeader, nil
}

// Check that the key hash exists in the database
func getKeyHash() (string, error) {
	keyhash := hex.EncodeToString(publicKey[:])
	hashes, err := db.ListKeyHashes()
	if err != nil {
		return "", err
	}
	found := false
	for n := range hashes {
		if hashes[n].Hash == keyhash && hashes[n].DeprecatedAt != "" {
			return "", errors.New("the c4gh key hash has been deprecated")
		}

		if hashes[n].Hash == keyhash && hashes[n].DeprecatedAt == "" {
			found = true

			break
		}
	}
	if !found {
		return "", errors.New("the c4gh key hash is not registered")
	}

	return keyhash, nil
}
