package broker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

// AMQPBroker is a Broker that reads messages from an AMQP broker
type AMQPBroker struct {
	Connection   *amqp.Connection
	Channel      *amqp.Channel
	Conf         MQConf
	confirmsChan <-chan amqp.Confirmation
}

// MQConf stores information about the message broker
type MQConf struct {
	Host         string
	Port         int
	User         string
	Password     string
	Vhost        string
	Queue        string
	Exchange     string
	RoutingKey   string
	RoutingError string
	Ssl          bool
	VerifyPeer   bool
	CACert       string
	ClientCert   string
	ClientKey    string
	ServerName   string
	Durable      bool
	SchemasPath  string
}

// buildMQURI builds the MQ connection URI
func buildMQURI(mqHost, mqUser, mqPassword, mqVhost string, mqPort int, ssl bool) string {
	protocol := "amqp"
	if ssl {
		protocol = "amqps"
	}

	return fmt.Sprintf("%s://%s:%s@%s:%d%s", protocol, mqUser, mqPassword, mqHost, mqPort, mqVhost)
}

// TLSConfigBroker is a helper method to setup TLS for the message broker
func TLSConfigBroker(config MQConf) (*tls.Config, error) {
	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}

	tlsConfig := tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    systemCAs,
	}

	// Add CAs for broker and database
	for _, cacert := range []string{config.CACert} {
		if cacert == "" {
			continue
		}
		cacert, err := os.ReadFile(cacert)
		if err != nil {
			return nil, err
		}
		if ok := tlsConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Warnln("No certs appended, using system certs only")
		}
	}

	// If the server URI difers from the hostname in the certificate
	// we need to set the hostname to match our certificates against.
	if config.ServerName != "" {
		tlsConfig.ServerName = config.ServerName
	}

	if config.VerifyPeer && config.ClientCert != "" && config.ClientKey != "" {
		cert, err := os.ReadFile(config.ClientCert)
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(config.ClientKey)
		if err != nil {
			return nil, err
		}
		certs, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = append(tlsConfig.Certificates, certs)
	}

	return &tlsConfig, nil
}

// NewMQ creates a new Broker that can communicate with a backend amqp server.
func NewMQ(config MQConf) (*AMQPBroker, error) {
	brokerURI := buildMQURI(config.Host, config.User, config.Password, config.Vhost, config.Port, config.Ssl)

	var connection *amqp.Connection
	var err error

	log.Debugf("Connecting to broker host: %s:%d vhost: %s with user: %s", config.Host, config.Port, config.Vhost, config.User)
	if config.Ssl {
		var tlsConfig *tls.Config
		tlsConfig, err = TLSConfigBroker(config)
		if err != nil {
			return nil, err
		}
		connection, err = amqp.DialTLS(brokerURI, tlsConfig)
	} else {
		connection, err = amqp.Dial(brokerURI)
	}
	if err != nil {
		return nil, err
	}

	channel, err := connection.Channel()
	if err != nil {
		return nil, err
	}
	if config.Queue != "" {
		// The queues already exists so we can safely do a passive declaration
		_, err = channel.QueueDeclarePassive(
			config.Queue, // name
			true,         // durable
			false,        // auto-deleted
			false,        // internal
			false,        // noWait
			nil,          // arguments
		)
		if err != nil {
			return nil, err
		}
	}

	if e := channel.Confirm(false); e != nil {
		fmt.Printf("channel could not be put into confirm mode: %s", e)

		return nil, fmt.Errorf("channel could not be put into confirm mode: %s", e)
	}

	confirms := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return &AMQPBroker{connection, channel, config, confirms}, nil
}

// ConnectionWatcher listens to events from the server
func (broker *AMQPBroker) ConnectionWatcher() *amqp.Error {
	amqpError := <-broker.Connection.NotifyClose(make(chan *amqp.Error))

	return amqpError
}

// GetMessages reads messages from the queue
func (broker *AMQPBroker) GetMessages(queue string) (<-chan amqp.Delivery, error) {
	ch := broker.Channel

	return ch.Consume(
		queue, // queue
		"",    // consumer
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
}

// SendMessage sends a message to RabbitMQ
func (broker *AMQPBroker) SendMessage(corrID, exchange, routingKey string, _ bool, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := broker.Channel.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         amqp.Table{},
			ContentEncoding: "UTF-8",
			ContentType:     "application/json",
			DeliveryMode:    amqp.Persistent, // 1=non-persistent, 2=persistent
			CorrelationId:   corrID,
			Priority:        0, // 0-9
			Body:            body,
			// a bunch of application/implementation-specific fields
		},
	)
	if err != nil {
		return err
	}

	confirmed := <-broker.confirmsChan
	if !confirmed.Ack {
		return fmt.Errorf("failed delivery of delivery tag: %d", confirmed.DeliveryTag)
	}
	log.Debugf("confirmed delivery with delivery tag: %d", confirmed.DeliveryTag)

	return nil
}

func (broker *AMQPBroker) CreateNewChannel() error {
	c, err := broker.Connection.Channel()
	if err != nil {
		return err
	}

	confirmsChan := make(chan amqp.Confirmation, 1)
	if err := c.Confirm(false); err != nil {
		close(confirmsChan)

		return fmt.Errorf("channel could not be put into confirm mode: %v", err)
	}

	log.Debugln("reconnected to new channel")
	broker.Channel = c
	broker.confirmsChan = c.NotifyPublish(confirmsChan)

	return nil
}

func (broker *AMQPBroker) IsConnClosed() bool {
	return broker.Connection.IsClosed()
}
