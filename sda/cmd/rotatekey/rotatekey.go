// The rotatekey service accepts messages to re-encrypt a file identified by its fileID.
// The service re-encrypts the file header with a configured public key and stores it
// in the database together with the key-hash of the rotation key.
// I then sends a message to verify so the file is re-verified.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

var (
	err       error
	publicKey *[32]byte
	db        *database.SDAdb
	Conf      *config.Config
)

func main() {
	forever := make(chan bool)
	Conf, err = config.NewConfig("rotatekey")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(Conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err = database.NewSDAdb(Conf.Database)
	if err != nil {
		log.Fatal(err)
	}

	publicKey, err = config.GetC4GHPublicKey("rotatekey")
	if err != nil {
		log.Fatal(err)
	}

	// Check that key is registered in the db at startup
	keyhash := hex.EncodeToString(publicKey[:])
	err = db.CheckKeyHash(keyhash)
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
	var message schema.KeyRotation

	go func() {
		messages, err := mq.GetMessages(Conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", Conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				msg := "validation of incoming message (rotate-key) failed"
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Fetch rotate key hash before starting work so that we make sure the hash state
			// has not changed since the application startup.
			keyhash := hex.EncodeToString(publicKey[:])
			err = db.CheckKeyHash(keyhash)
			// exit app if target key was modified after app start-up, e.g. if key has been deprecated
			if err != nil {
				log.Fatalf("check of target key failed, reason: %v", err)
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			fileID := message.FileID

			// Get current keyhash for the file, send to error queue if this fails
			oldKeyHash, err := db.GetKeyHash(fileID)
			if err != nil {
				msg := fmt.Sprintf("failed to get keyhash for file with file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Check that the file is not already encrypted with the target key
			if oldKeyHash == keyhash {
				log.Infof("the file with file-id: %s is already encrypted with the given rotation c4gh key", fileID)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack following already encrypted with key message")
				}

				continue
			}

			newHeader, err := reencryptFile(fileID)
			if err != nil {
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}
			if newHeader == nil {
				err := errors.New("reencrypt returned empty header")
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Rotate header in database
			if err := db.StoreHeader(newHeader, fileID); err != nil {
				msg := fmt.Sprintf("StoreHeader failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Rotate keyhash
			if err := db.SetKeyHash(keyhash, fileID); err != nil {
				msg := fmt.Sprintf("SetKeyHash failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			aID, err := db.GetAccessionID(fileID)
			if err != nil {
				msg := fmt.Sprintf("GetAccessionID failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Send re-verify message
			reVerify, err := db.GetReVerificationData(aID)
			if err != nil {
				msg := fmt.Sprintf("GetReVerificationData failed for file-id %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			reVerifyMsg, _ := json.Marshal(&reVerify)
			err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", Conf.Broker.SchemasPath), reVerifyMsg)
			if err != nil {
				msg := "Validation of outgoing re-verify message failed"
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			if err := mq.SendMessage(delivered.CorrelationId, Conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
				msg := "failed to publish message"
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}
		}
	}()

	<-forever
}

func reencryptFile(fileID string) ([]byte, error) {
	log.Debugf("rotating c4gh key for file with file-id: %s", fileID)

	header, err := db.GetHeader(fileID)
	if err != nil {
		return nil, err
	}

	// encode pubkey as pem and then as base64 string
	tmp := &bytes.Buffer{}
	if err = keys.WriteCrypt4GHX25519PublicKey(tmp, *publicKey); err != nil {
		return nil, err
	}
	pubKeyEncoded := base64.StdEncoding.EncodeToString(tmp.Bytes())

	newHeader, err := reencrypt.CallReencryptHeader(header, pubKeyEncoded, Conf.RotateKey.Grpc)

	if err != nil {
		return nil, err
	}

	return newHeader, nil
}

// Nack message without requeue. Send the message to an error queue so it can be analyzed.
func NackAndSendToErrorQueue(mq *broker.AMQPBroker, delivered amqp091.Delivery, msg, reason string) {
	infoErrorMessage := broker.InfoError{
		Error:           msg,
		Reason:          reason,
		OriginalMessage: string(delivered.Body),
	}
	body, _ := json.Marshal(infoErrorMessage)

	if err := mq.SendMessage(delivered.CorrelationId, Conf.Broker.Exchange, "error", body); err != nil {
		log.Errorf("failed to publish message, reason: (%s)", err.Error())
	}
	if err := delivered.Ack(false); err != nil {
		log.Errorf("failed to Ack message, reason: (%s)", err.Error())
	}
}
