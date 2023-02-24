// Notify service, for sending email notifications
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/smtp"
	"os"
	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/common"
	"sda-pipeline/internal/config"
	"strconv"

	"github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

const err = "error"
const ready = "ready"

func main() {
	conf, err := config.NewConfig("notify")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		os.Exit(1)
	}()

	forever := make(chan bool)

	log.Infof("Starting %s notify service", conf.Broker.Queue)

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get message from mq (error: %v)", err)
		}

		for d := range messages {
			log.Debugf("received a message: %s", d.Body)

			if err := validator(conf.Broker.Queue, conf.Broker.SchemasPath, d); err != nil {
				log.Errorf("Failed to handle message, reason: %v", err)

				continue
			}
			user := getUser(conf.Broker.Queue, d.Body)
			if user == "" {
				log.Errorln("No user in message, skipping")

				continue
			}

			if err := sendEmail(conf.Notify, "THIS SHOULD TAKE A TEMPLATE", user, setSubject(conf.Broker.Queue)); err != nil {
				log.Errorf("Failed to send email, error %v", err)

				if e := d.Nack(false, false); e != nil {
					log.Errorf("Failed to Nack message (corr-id: %s, errror: %v) ", d.CorrelationId, e)
				}

				continue
			}

			if err := d.Ack(false); err != nil {
				log.Errorf("Failed to ack message, error %v", err)
			}
		}
	}()

	<-forever
}

func getUser(queue string, orgMsg []byte) string {
	switch queue {
	case err:
		var notify broker.InfoError
		_ = json.Unmarshal(orgMsg, &notify)
		orgMsg, _ := base64.StdEncoding.DecodeString(notify.OriginalMessage.(string))

		var message map[string]interface{}
		_ = json.Unmarshal(orgMsg, &message)

		return fmt.Sprint(message["user"])
	case ready:
		var notify common.Completed
		_ = json.Unmarshal(orgMsg, &notify)

		return notify.User
	default:
		return ""
	}
}

func sendEmail(conf config.SMTPConf, emailBody, recipient, subject string) error {
	// Receiver email address.
	to := []string{recipient}

	// smtp server configuration.
	smtpHost := conf.Host
	smtpPort := strconv.Itoa(conf.Port)

	// Message.
	message := []byte(emailBody)

	// Authentication.
	auth := smtp.PlainAuth("", conf.FromAddr, conf.Password, smtpHost)

	// Sending email.
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, conf.FromAddr, to, message)
	if err != nil {
		return err
	}

	return nil
}

func setSubject(queue string) string {
	switch queue {
	case err:
		return "Error during ingestion"
	case ready:
		return "Ingestion completed"
	default:
		return ""
	}
}

func validator(queue, schemaPath string, delivery amqp091.Delivery) error {
	switch queue {
	case err:
		reference := schemaPath + "/" + "info-error.json"
		res, err := common.ValidateJSON(reference, delivery.Body)
		if err != nil {
			return err
		}
		if !res.Valid() {
			errorString := ""

			for _, validErr := range res.Errors() {
				errorString += validErr.String() + "\n\n"
			}

			return fmt.Errorf(errorString)
		}

		return nil
	case ready:
		reference := schemaPath + "/" + "ingestion-completion.json"
		res, err := common.ValidateJSON(reference, delivery.Body)
		if err != nil {
			return err
		}

		if !res.Valid() {
			errorString := ""

			for _, validErr := range res.Errors() {
				errorString += validErr.String() + "\n\n"
			}

			return fmt.Errorf(errorString)
		}

		return nil
	}

	return fmt.Errorf("Error")
}
