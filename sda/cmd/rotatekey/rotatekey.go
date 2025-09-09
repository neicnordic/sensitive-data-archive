// The rotatekey service accepts messages for files mapped to a dataset,
// re-encrypts their header with a configured public key and stores it
// in the database together with the key-hash of the rotation key.
// I then sends a message to verify so the file is re-verified.

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	err       error
	publicKey *[32]byte
	db        *database.SDAdb
	conf      *config.Config
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
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following failed to get fileID from accessionID error message")
					}

					continue
				}

				// Get current keyhash for the file, send to error queue if this fails
				oldKeyHash, err := db.GetKeyHash(fileID)
				if err != nil {
					log.Errorf("failed to get keyhash for file with accession-id: %s, reason: %v", aID, err)
					// Send the message to an error queue so it can be analyzed.
					infoErrorMessage := broker.InfoError{
						Error:           "Failed to get source key hash in rotatekey service",
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
				if newHeader == nil {
					err = errors.New("reencrypt returned empty header")
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
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack following GetReVerificationData error message")
					}

					continue
				}

				reVerifyMsg, _ := json.Marshal(&reVerify)
				err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", conf.Broker.SchemasPath), reVerifyMsg)
				if err != nil {
					log.Errorf("Validation of outgoing re-verify message failed, reason: %v", err)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack after verify schema validation error message")
					}

					continue
				}

				corrID, err := db.GetCorrID(reVerify.User, reVerify.FilePath, aID)
				if err != nil {
					log.Errorf("failed to get CorrID for %s, %s", reVerify.User, reVerify.FilePath)
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack after GetCorrID error message")
					}

					continue
				}

				if err := mq.SendMessage(corrID, conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
					if err := delivered.Nack(false, false); err != nil {
						log.Errorf("failed to nack after SendMessage error message")
					}

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

	// encode pubkey as pem and then as base64 string
	tmp := &bytes.Buffer{}
	if err = keys.WriteCrypt4GHX25519PublicKey(tmp, *publicKey); err != nil {
		return nil, err
	}
	pubKeyEncoded := base64.StdEncoding.EncodeToString(tmp.Bytes())

	newHeader, err := reencryptHeader(header, pubKeyEncoded)
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

// reencryptHeader re-encrypts the header of a file using the public key
// provided in the request header and returns the new header. The function uses
// gRPC to communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate is
// provided for secure communication.
func reencryptHeader(oldHeader []byte, c4ghPubKey string) ([]byte, error) {
	var opts []grpc.DialOption
	switch {
	case conf.RotateKey.Grpc.ClientCreds != nil:
		opts = append(opts, grpc.WithTransportCredentials(conf.RotateKey.Grpc.ClientCreds))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", conf.RotateKey.Grpc.Host, conf.RotateKey.Grpc.Port), opts...)
	if err != nil {
		log.Errorf("failed to connect to the reencrypt service, reason: %s", err)

		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(conf.RotateKey.Grpc.Timeout)*time.Second)
	defer cancel()

	c := reencrypt.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{Oldheader: oldHeader, Publickey: c4ghPubKey})
	if err != nil {
		return nil, err
	}

	return res.Header, nil
}
