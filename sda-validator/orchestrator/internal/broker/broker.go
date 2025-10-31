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

// amqpBroker is a Broker that reads messages from an AMQP broker
type amqpBroker struct {
	connection *amqp.Connection
	channel    *amqp.Channel

	config             *amqpConfig
	publishConfirmChan <-chan amqp.Confirmation
}

// NewAMQPBroker creates a new Broker that can communicate with a backend amqp server.
func NewAMQPBroker(options ...func(*amqpConfig)) (AMQPBrokerI, error) {
	brokerConf := globalConf.clone()

	for _, o := range options {
		o(brokerConf)
	}

	brokerURI := globalConf.buildMQURI()

	var connection *amqp.Connection
	var err error

	log.Debugf("Connecting to broker host: %s:%d vhost: %s with user: %s", globalConf.host, globalConf.port, globalConf.vhost, globalConf.user)
	if globalConf.ssl {
		var tlsConfig *tls.Config
		tlsConfig, err = setupTLSConfig(brokerConf)
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

	if e := channel.Confirm(false); e != nil {
		_, _ = fmt.Printf("channel could not be put into confirm mode: %s", e)

		return nil, fmt.Errorf("channel could not be put into confirm mode: %s", e)
	}

	if globalConf.prefetchCount > 0 {
		// limit the number of messages retrieved from the queue
		log.Debugf("prefetch count: %v", globalConf.prefetchCount)
		if err := channel.Qos(globalConf.prefetchCount, 0, false); err != nil {
			log.Errorf("failed to set channel QoS to %d, reason: %v", globalConf.prefetchCount, err)
		}
	}

	confirms := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return &amqpBroker{
		connection:         connection,
		channel:            channel,
		config:             brokerConf,
		publishConfirmChan: confirms,
	}, nil
}

// setupTLSConfig is a helper method to setup TLS for the message broker
func setupTLSConfig(conf *amqpConfig) (*tls.Config, error) {
	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	tlsConfig := tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    systemCAs,
	}

	// Add CAs for broker and database
	if conf.cACert != "" {
		cacert, err := os.ReadFile(conf.cACert)
		if err != nil {
			return nil, err
		}
		if ok := tlsConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Warnln("No certs appended, using system certs only")
		}
	}

	// If the server URI difers from the hostname in the certificate
	// we need to set the hostname to match our certificates against.
	if conf.serverName != "" {
		tlsConfig.ServerName = conf.serverName
	}

	if conf.verifyPeer && conf.clientCert != "" && conf.clientKey != "" {
		cert, err := os.ReadFile(conf.clientCert)
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(conf.clientKey)
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

func (broker *amqpBroker) Subscribe(ctx context.Context, queue, consumerID string, handleFunc func(context.Context, amqp.Delivery) error) error {
	messageChan, err := broker.channel.ConsumeWithContext(ctx,
		queue,      // queue
		consumerID, // consumerID
		false,      // auto-ack
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		return err
	}

	for {
		select {
		case message, ok := <-messageChan:
			if !ok {
				log.Debugf("consumerID: %s, channel closed", consumerID)

				return nil
			}
			if err := handleFunc(context.Background(), message); err != nil {
				if err := message.Nack(false, true); err != nil {
					log.Errorf("consumerID: %s, failed to nack message, reason: %v", consumerID, err)
				}

				continue
			}
			if err := message.Ack(false); err != nil {
				log.Errorf("consumerID: %s, failed to ack message, reason: %v", consumerID, err)
			}
		case <-ctx.Done():
			// Here we cancel the consumerID and let loop continue until messageChan has closed
			if err := broker.channel.Cancel(consumerID, true); err != nil {
				log.Errorf("failed to cancel channel for consumerID: %s, reason: %v", consumerID, err)
			}
		}
	}
}

// PublishMessage sends a message to RabbitMQ queue
func (broker *amqpBroker) PublishMessage(ctx context.Context, destination string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := broker.channel.PublishWithContext(
		ctx,
		broker.config.exchange,
		destination,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         amqp.Table{},
			ContentEncoding: "UTF-8",
			ContentType:     "application/json",
			DeliveryMode:    amqp.Persistent, // 1=non-persistent, 2=persistent
			Body:            body,
			Timestamp:       time.Now(),
		},
	)
	if err != nil {
		return err
	}

	confirmed := <-broker.publishConfirmChan
	if !confirmed.Ack {
		return fmt.Errorf("failed delivery of delivery tag: %d", confirmed.DeliveryTag)
	}
	log.Debugf("published message to %s, with delivery tag: %d", destination, confirmed.DeliveryTag)

	return nil
}

func (broker *amqpBroker) Close() error {
	if err := broker.channel.Close(); err != nil {
		return err
	}

	return broker.connection.Close()
}

// ConnectionWatcher listens to events from the server
func (broker *amqpBroker) ConnectionWatcher() chan *amqp.Error {
	return broker.connection.NotifyClose(make(chan *amqp.Error))
}
