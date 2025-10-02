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
	"strings"
	"syscall"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
)

type RotateKey struct {
	Conf          *config.Config
	MQ            *broker.AMQPBroker
	DB            *database.SDAdb
	PubKeyEncoded string
}

func main() {
	app := RotateKey{}
	var err error

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Create a function to handle panic and exit gracefully
	defer func() {
		if err := recover(); err != nil {
			if app.MQ != nil {
				defer app.MQ.Channel.Close()
				defer app.MQ.Connection.Close()
			}
			if app.DB != nil {
				defer app.DB.Close()
			}
			log.Fatal(err)
		}
	}()

	forever := make(chan bool)

	app.Conf, err = config.NewConfig("rotatekey")
	if err != nil {
		panic(err)
	}
	app.MQ, err = broker.NewMQ(app.Conf.Broker)
	if err != nil {
		panic(err)
	}
	app.DB, err = database.NewSDAdb(app.Conf.Database)
	if err != nil {
		panic(err)
	}

	go func() {
		<-sigc // blocks here until it receives from sigc
		fmt.Println("Interrupt signal received. Shutting down.")
		defer app.MQ.Channel.Close()
		defer app.MQ.Connection.Close()
		defer app.DB.Close()

		os.Exit(0) // exit program
	}()

	// encode pubkey as pem and then as base64 string
	tmp := &bytes.Buffer{}
	if err := keys.WriteCrypt4GHX25519PublicKey(tmp, *app.Conf.RotateKey.PublicKey); err != nil {
		panic(err)
	}
	app.PubKeyEncoded = base64.StdEncoding.EncodeToString(tmp.Bytes())

	// Check that key is registered in the db at startup
	err = app.DB.CheckKeyHash(hex.EncodeToString(app.Conf.RotateKey.PublicKey[:]))
	if err != nil {
		panic(fmt.Errorf("database lookup of the rotation key failed, reason: %v", err))
	}

	go func() {
		connError := app.MQ.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := app.MQ.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("Starting rotatekey service")
	var message schema.KeyRotation

	go func() {
		// Create a function to handle panic and exit gracefully
		defer func() {
			if err := recover(); err != nil {
				if app.MQ != nil {
					defer app.MQ.Channel.Close()
					defer app.MQ.Connection.Close()
				}
				if app.DB != nil {
					defer app.DB.Close()
				}
				log.Fatal(err)
			}
		}()
		messages, err := app.MQ.GetMessages(app.Conf.Broker.Queue)
		if err != nil {
			panic(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)",
				delivered.CorrelationId,
				delivered.Body)

			err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", app.Conf.Broker.SchemasPath), delivered.Body)
			if err != nil {
				msg := "validation of incoming message (rotate-key) failed"
				log.Errorf("%s, reason: %v", msg, err)
				// Ack message and send the payload to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           msg,
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}
				body, _ := json.Marshal(infoErrorMessage)
				if err := app.MQ.SendMessage(delivered.CorrelationId, app.Conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			// Fetch rotate key hash before starting work so that we make sure the hash state
			// has not changed since the application startup.
			keyhash := hex.EncodeToString(app.Conf.RotateKey.PublicKey[:])
			// exit app if target key was modified after app start-up, e.g. if key has been deprecated
			if err = app.DB.CheckKeyHash(keyhash); err != nil {
				panic(fmt.Errorf("check of target key failed, reason: %v", err))
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &message)

			ackNack, msg, err := app.rotateHeader(delivered.CorrelationId, message.FileID)

			switch ackNack {
			case "ack":
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message, reason: %v", err)
				}
			case "ackSendToError":
				infoErrorMessage := broker.InfoError{
					Error:           msg,
					Reason:          err.Error(),
					OriginalMessage: string(delivered.Body),
				}
				body, _ := json.Marshal(infoErrorMessage)
				if err := app.MQ.SendMessage(delivered.CorrelationId, app.Conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}
			case "nackRequeue":
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: %v", err)
				}
			default:
				// will catch `reject`s, failures that should not be requeued.
				if err := delivered.Reject(false); err != nil {
					log.Errorf("failed to reject message, reason: %v", err)
				}
			}
		}
	}()

	<-forever
}

func (app *RotateKey) rotateHeader(correlationID, fileID string) (ackNack, msg string, err error) {
	// Get current keyhash for the file, send to error queue if this fails
	oldKeyHash, err := app.DB.GetKeyHash(fileID)
	if err != nil {
		msg := fmt.Sprintf("failed to get keyhash for file with file-id: %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		switch {
		case strings.Contains(err.Error(), "sql: no rows in result set"):
			return "ackSendToError", msg, err
		default:
			return "nackRequeue", msg, err
		}
	}

	// Check that the file is not already encrypted with the target key
	keyhash := hex.EncodeToString(app.Conf.RotateKey.PublicKey[:])
	if oldKeyHash == keyhash {
		log.Infof("the file with file-id: %s is already encrypted with the given rotation c4gh key", fileID)

		return "ack", "", nil
	}

	// reencrypt header
	log.Debugf("rotating c4gh key for file with file-id: %s", fileID)

	header, err := app.DB.GetHeader(fileID)
	if err != nil {
		msg := fmt.Sprintf("GetHeader failed for file-id: %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		switch {
		case strings.Contains(err.Error(), "sql: no rows in result set"):
			return "ackSendToError", msg, err
		default:
			return "nackRequeue", msg, err
		}
	}

	newHeader, err := reencrypt.CallReencryptHeader(header, app.PubKeyEncoded, app.Conf.RotateKey.Grpc)
	if err != nil {
		msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		return "ackSendToError", msg, err
	}
	if newHeader == nil {
		err := errors.New("reencrypt returned empty header")
		msg := fmt.Sprintf("failed to rotate c4gh key for file %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		return "ackSendToError", msg, err
	}

	// Rotate header and keyhash in database
	if err := app.DB.RotateHeaderKey(newHeader, keyhash, fileID); err != nil {
		msg := fmt.Sprintf("RotateHeaderKey failed for file-id: %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		return "nackRequeue", msg, err
	}

	aID, err := app.DB.GetAccessionID(fileID)
	if err != nil {
		msg := fmt.Sprintf("GetAccessionID failed for file-id: %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		return "ackSendToError", msg, err
	}

	// Send re-verify message
	reVerify, err := app.DB.GetReVerificationData(aID)
	if err != nil {
		msg := fmt.Sprintf("GetReVerificationData failed for file-id %s", fileID)
		log.Errorf("%s, reason: %v", msg, err)

		return "ackSendToError", msg, err
	}

	reVerifyMsg, _ := json.Marshal(&reVerify)
	err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.Conf.Broker.SchemasPath), reVerifyMsg)
	if err != nil {
		msg := "Validation of outgoing re-verify message failed"
		log.Errorf("%s, reason: %v", msg, err)

		return "ackSendToError", msg, err
	}

	if err := app.MQ.SendMessage(correlationID, app.Conf.Broker.Exchange, "archived", reVerifyMsg); err != nil {
		msg := "failed to publish message"
		log.Errorf("%s, reason: %v", msg, err)

		return "nackRequeue", msg, err
	}

	return "ack", "", nil
}
