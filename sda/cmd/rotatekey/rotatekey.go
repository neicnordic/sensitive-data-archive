// The rotatekey service accepts messages to re-encrypt a file identified by its fileID.
// The service re-encrypts the file header with a configured public key and stores it
// in the database together with the key-hash of the rotation key.
// It then sends a message to verify so that the file is re-verified.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

func main() {
	var (
		mq *broker.AMQPBroker
		db *database.SDAdb
	)
	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Create a function to handle panic and exit gracefully
	defer func() {
		if err := recover(); err != nil {
			if mq != nil {
				defer mq.Channel.Close()
				defer mq.Connection.Close()
			}
			if db != nil {
				defer db.Close()
			}
			log.Fatal(err)
		}
	}()

	forever := make(chan bool)

	conf, err := config.NewConfig("rotatekey")
	if err != nil {
		panic(err)
	}
	mq, err = broker.NewMQ(conf.Broker)
	if err != nil {
		panic(err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		panic(err)
	}

	go func() {
		<-sigc // blocks here until it receives from sigc
		fmt.Println("Interrupt signal received. Shutting down.")
		defer mq.Channel.Close()
		defer mq.Connection.Close()
		defer db.Close()

		os.Exit(0) // exit program
	}()

	// encode pubkey as pem and then as base64 string
	tmp := &bytes.Buffer{}
	if err := keys.WriteCrypt4GHX25519PublicKey(tmp, *conf.RotateKey.PublicKey); err != nil {
		panic(err)
	}
	pubKeyEncoded := base64.StdEncoding.EncodeToString(tmp.Bytes())

	// Check that key is registered in the db at startup
	err = db.CheckKeyHash(hex.EncodeToString(conf.RotateKey.PublicKey[:]))
	if err != nil {
		panic(fmt.Errorf("database lookup of the rotation key failed, reason: %v", err))
	}

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
		// Create a function to handle panic and exit gracefully
		defer func() {
			if err := recover(); err != nil {
				if mq != nil {
					defer mq.Channel.Close()
					defer mq.Connection.Close()
				}
				if db != nil {
					defer db.Close()
				}
				log.Fatal(err)
			}
		}()
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			panic(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				msg := "validation of incoming message (rotate-key) failed"
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			// Fetch rotate key hash before starting work so that we make sure the hash state
			// has not changed since the application startup.
			keyhash := hex.EncodeToString(conf.RotateKey.PublicKey[:])
			// exit app if target key was modified after app start-up, e.g. if key has been deprecated
			if err = db.CheckKeyHash(keyhash); err != nil {
				panic(fmt.Errorf("check of target key failed, reason: %v", err))
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			fileID := message.FileID

			// Get current keyhash for the file, send to error queue if this fails
			oldKeyHash, err := db.GetKeyHash(fileID)
			if err != nil {
				msg := fmt.Sprintf("failed to get keyhash for file with file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

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

			// reencrypt header
			log.Debugf("rotating c4gh key for file with file-id: %s", fileID)

			header, err := db.GetHeader(fileID)
			if err != nil {
				msg := fmt.Sprintf("GetHeader failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			newHeader, err := reencrypt.CallReencryptHeader(header, pubKeyEncoded, conf.RotateKey.Grpc)
			if err != nil {
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}
			if newHeader == nil {
				err := errors.New("reencrypt returned empty header")
				msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			// Rotate header in database
			if err := db.StoreHeader(newHeader, fileID); err != nil {
				msg := fmt.Sprintf("StoreHeader failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				NackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			// Rotate keyhash
			if err := db.SetKeyHash(keyhash, fileID); err != nil {
				msg := fmt.Sprintf("SetKeyHash failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			aID, err := db.GetAccessionID(fileID)
			if err != nil {
				msg := fmt.Sprintf("GetAccessionID failed for file-id: %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			// Send re-verify message
			reVerify, err := db.GetReVerificationData(aID)
			if err != nil {
				msg := fmt.Sprintf("GetReVerificationData failed for file-id %s", fileID)
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			reVerifyMsg, _ := json.Marshal(&reVerify)
			err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", conf.Broker.SchemasPath), reVerifyMsg)
			if err != nil {
				msg := "Validation of outgoing re-verify message failed"
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
				msg := "failed to publish message"
				log.Errorf("%s, reason: %v", msg, err)
				nackAndSendToErrorQueue(mq, delivered, conf.Broker.Exchange, msg, err.Error())

				continue
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%s)", err.Error())
			}
		}
	}()

	<-forever
}

// Nack message and send the payload to an error queue so it can be analyzed.
func nackAndSendToErrorQueue(mq *broker.AMQPBroker, delivered amqp091.Delivery, mqExchange, msg, reason string) {
	infoErrorMessage := broker.InfoError{
		Error:           msg,
		Reason:          reason,
		OriginalMessage: string(delivered.Body),
	}
	body, _ := json.Marshal(infoErrorMessage)

	if err := mq.SendMessage(delivered.CorrelationId, mqExchange, "error", body); err != nil {
		log.Errorf("failed to publish message, reason: (%s)", err.Error())
	}
	if err := delivered.Nack(false, false); err != nil {
		log.Errorf("failed to Ack message, reason: (%s)", err.Error())
	}
}
