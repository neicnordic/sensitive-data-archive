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
	"github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	var message schema.DatasetMapping

	go func() {
		messages, err := mq.GetMessages(Conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", Conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				msg := "validation of incoming message (dataset-mapping) failed"
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Fetch rotate key hash before starting work so that we make sure the hash state
			// has not changed since the application startup.
			keyhash := hex.EncodeToString(publicKey[:])
			err = db.CheckKeyHash(keyhash)
			if err != nil {
				msg := "database lookup of the rotation key failed"
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			// We expect only one aID per message so that we handle errors and nacks properly.
			// A different json schema seems like a cleaner solution going forward.
			if len(message.AccessionIDs) > 1 {
				log.Errorf("failed to process message, reason: multiple accession_id's per message is not supported")
				NackAndSendToErrorQueue(mq, delivered, "failed to process message", "multiple accession_id's per message is not supported")

				continue
			}

			aID := message.AccessionIDs[0]

			fileID, err := db.GetFileIDbyAccessionID(aID)
			if err != nil {
				msg := fmt.Sprintf("failed to get file-id for file with accession-id: %s", aID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			// Get current keyhash for the file, send to error queue if this fails
			oldKeyHash, err := db.GetKeyHash(fileID)
			if err != nil {
				msg := fmt.Sprintf("failed to get keyhash for file with accession-id: %s", aID)
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

			newHeader, err := reencryptFile(aID)
			if err != nil {
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", aID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}
			if newHeader == nil {
				err := errors.New("reencrypt returned empty header")
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", aID)
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

			corrID, err := db.GetCorrID(reVerify.User, reVerify.FilePath, aID)
			if err != nil {
				msg := fmt.Sprintf("failed to get CorrID for %s, %s", reVerify.User, reVerify.FilePath)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, msg, err.Error())

				continue
			}

			if err := mq.SendMessage(corrID, Conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
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

func reencryptFile(stableID string) ([]byte, error) {
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

// reencryptHeader re-encrypts the header of a file using the public key
// provided in the request header and returns the new header. The function uses
// gRPC to communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate is
// provided for secure communication.
func reencryptHeader(oldHeader []byte, c4ghPubKey string) ([]byte, error) {
	var opts []grpc.DialOption
	switch {
	case Conf.RotateKey.Grpc.ClientCreds != nil:
		opts = append(opts, grpc.WithTransportCredentials(Conf.RotateKey.Grpc.ClientCreds))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", Conf.RotateKey.Grpc.Host, Conf.RotateKey.Grpc.Port), opts...)
	if err != nil {
		log.Errorf("failed to connect to the reencrypt service, reason: %s", err)

		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(Conf.RotateKey.Grpc.Timeout)*time.Second)
	defer cancel()

	c := reencrypt.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{Oldheader: oldHeader, Publickey: c4ghPubKey})
	if err != nil {
		return nil, err
	}

	return res.Header, nil
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
