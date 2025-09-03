// Notify service, for sending email notifications
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strconv"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const ready = "ready"

var (
	mq   *broker.AMQPBroker
	conf *config.Config
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := observability.SetupOTelSDK(ctx, "notify")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		if err := otelShutdown(ctx); err != nil {
			log.Errorf("failed to shutdown otel: %v", err)
		}
	}()

	ctx, span := observability.GetTracer().Start(ctx, "startUp")

	forever := make(chan bool)
	conf, err = config.NewConfig("notify")
	if err != nil {
		log.Fatal(err)
	}
	mq, err = broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()

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

	log.Infof("Starting %s notify service", conf.Broker.Queue)
	span.End()

	go func() {
		messages, err := mq.GetMessages(ctx, conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get message from mq (error: %v)", err)
		}

		for msg := range messages {
			ctx, span := observability.GetTracer().Start(msg.Context(), "handleMessage", trace.WithAttributes(attribute.String("correlation-id", msg.Message.CorrelationId)))

			if err := handleMessage(ctx, msg.Message); err != nil {
				// TODO err handle
				span.End()
				log.Fatal(err)
			}

			span.End()
		}
	}()

	<-forever
}

func getUser(queue string, orgMsg []byte) string {
	switch queue {
	case "error":
		var notify broker.InfoError
		_ = json.Unmarshal(orgMsg, &notify)
		orgMsg, _ := base64.StdEncoding.DecodeString(notify.OriginalMessage.(string))

		var message map[string]any
		_ = json.Unmarshal(orgMsg, &message)

		return fmt.Sprint(message["user"])
	case ready:
		var notify schema.IngestionCompletion
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
	case "error":
		return "Error during ingestion"
	case ready:
		return "Ingestion completed"
	default:
		return ""
	}
}

func validator(queue, schemaPath string, delivery amqp.Delivery) error {
	switch queue {
	case "error":
		if err := schema.ValidateJSON(fmt.Sprintf("%s/info-error.json", schemaPath), delivery.Body); err != nil {
			return err
		}

		return nil
	case ready:
		if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-completion.json", schemaPath), delivery.Body); err != nil {
			return err
		}

		return nil
	default:
		return fmt.Errorf("unknown queue, %s", queue)
	}
}

func handleMessage(ctx context.Context, delivered amqp.Delivery) error {
	log.Debugf("received a message: %s", delivered.Body)

	if err := validator(conf.Broker.Queue, conf.Broker.SchemasPath, delivered); err != nil {
		log.Errorf("Failed to handle message, reason: %v", err)

		return nil
	}
	user := getUser(conf.Broker.Queue, delivered.Body)
	if user == "" {
		log.Errorln("No user in message, skipping")

		return nil
	}

	if err := sendEmail(conf.Notify, "THIS SHOULD TAKE A TEMPLATE", user, setSubject(conf.Broker.Queue)); err != nil {
		log.Errorf("Failed to send email, error %v", err)

		if e := delivered.Nack(false, false); e != nil {
			log.Errorf("Failed to Nack message, error: %v) ", e)
		}

		return nil
	}

	if err := delivered.Ack(false); err != nil {
		log.Errorf("Failed to ack message, error %v", err)
	}

	return nil
}
