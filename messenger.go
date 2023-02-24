package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

// Checksum used in the message
type Checksum struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// The Event struct
type Event struct {
	Operation string        `json:"operation"`
	Username  string        `json:"user"`
	Filepath  string        `json:"filepath"`
	Filesize  int64         `json:"filesize"`
	Checksum  []interface{} `json:"encrypted_checksums"`
}

// Messenger is an interface for sending messages for different file events
type Messenger interface {
	SendMessage(string, []byte) error
	IsConnClosed() bool
}

// AMQPMessenger is a Messenger that sends messages to a local AMQP broker
type AMQPMessenger struct {
	connection   *amqp.Connection
	channel      *amqp.Channel
	exchange     string
	routingKey   string
	confirmsChan <-chan amqp.Confirmation
}

// NewAMQPMessenger creates a new messenger that can communicate with a backend amqp server.
func NewAMQPMessenger(c BrokerConfig, tlsConfig *tls.Config) (*AMQPMessenger, error) {
	brokerURI := buildMqURI(c.host, c.port, c.user, c.password, c.vhost, c.ssl)

	var connection *amqp.Connection
	var channel *amqp.Channel
	var err error

	log.Debugf("connecting to broker with <%s>", brokerURI)
	if c.ssl {
		connection, err = amqp.DialTLS(brokerURI, tlsConfig)
	} else {
		connection, err = amqp.Dial(brokerURI)
	}
	if err != nil {
		return nil, fmt.Errorf("brokerErrMsg 1: %s", err)
	}

	channel, err = connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("brokerErrMsg 2: %s", err)
	}

	log.Debug("enabling publishing confirms.")
	if err = channel.Confirm(false); err != nil {
		log.Fatalf("channel could not be put into confirm mode: %s", err)
	}

	if err = channel.ExchangeDeclarePassive(
		c.exchange, // name
		"topic",    // type
		true,       // durable
		false,      // auto-deleted
		false,      // internal
		false,      // noWait
		nil,        // arguments
	); err != nil {
		log.Fatalf("exchange declare: %s", err)
	}

	confirmsChan := make(chan amqp.Confirmation, 1)
	if err := channel.Confirm(false); err != nil {
		close(confirmsChan)
		log.Fatalf("Channel could not be put into confirm mode: %s\n", err)
	}

	return &AMQPMessenger{connection, channel, c.exchange, c.routingKey, channel.NotifyPublish(confirmsChan)}, err
}

// SendMessage sends message to RabbitMQ if the upload is finished
func (m *AMQPMessenger) SendMessage(corrID string, body []byte) error {
	if m.channel.IsClosed() {
		log.Debugln("channel closed, reconnecting")
		if err := m.createNewChannel(); err != nil {
			return fmt.Errorf("failed to recreate channel: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := m.channel.PublishWithContext(
		ctx,
		m.exchange,
		m.routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         amqp.Table{},
			ContentEncoding: "UTF-8",
			ContentType:     "application/json",
			DeliveryMode:    amqp.Persistent,
			CorrelationId:   corrID,
			Priority:        0, // 0-9
			Body:            body,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to send message because: %v", err)
	}

	confirmed := <-m.confirmsChan
	if !confirmed.Ack {
		return fmt.Errorf("failed delivery of delivery tag: %d", confirmed.DeliveryTag)
	}
	log.Debugf("Delivered message: %v, with correlation-ID: %v", string(body), corrID)

	return nil

}

// BuildMqURI builds the MQ URI
func buildMqURI(mqHost, mqPort, mqUser, mqPassword, mqVhost string, ssl bool) string {
	brokerURI := ""
	if ssl {
		brokerURI = "amqps://" + mqUser + ":" + mqPassword + "@" + mqHost + ":" + mqPort + mqVhost
	} else {
		brokerURI = "amqp://" + mqUser + ":" + mqPassword + "@" + mqHost + ":" + mqPort + mqVhost
	}

	return brokerURI
}

func (m *AMQPMessenger) createNewChannel() error {
	c, err := m.connection.Channel()
	if err != nil {
		return err
	}
	confirmsChan := make(chan amqp.Confirmation, 1)
	if err := c.Confirm(false); err != nil {
		close(confirmsChan)

		return fmt.Errorf("channel could not be put into confirm mode: %v", err)
	}
	log.Debugln("reconnected to new channel")
	m.channel = c
	m.confirmsChan = c.NotifyPublish(confirmsChan)

	return nil
}

func (m *AMQPMessenger) IsConnClosed() bool {
	return m.connection.IsClosed()
}
