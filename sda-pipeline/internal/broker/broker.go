// Package broker contains logic for communicating with a message broker
package broker

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/xeipuuv/gojsonschema"

	amqp "github.com/rabbitmq/amqp091-go"
)

// This is an internal helper variable to make testing easier
var logFatalf = log.Fatalf

// The AMQPChannel interface gives access to the functions provided
type AMQPChannel interface {
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Confirm(noWait bool) error
	NotifyClose(c chan *amqp.Error) chan *amqp.Error
	NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation
	Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Close() error
	IsClosed() bool
}

// AMQPBroker is a Broker that reads messages from an AMQP broker
type AMQPBroker struct {
	Connection   *amqp.Connection
	Channel      AMQPChannel
	Conf         MQConf
	confirmsChan <-chan amqp.Confirmation
}

// MQConf stores information about the message broker
type MQConf struct {
	Host               string
	Port               int
	User               string
	Password           string
	Vhost              string
	Queue              string
	Exchange           string
	RoutingKey         string
	RoutingError       string
	Ssl                bool
	InsecureSkipVerify bool
	VerifyPeer         bool
	CACert             string
	ClientCert         string
	ClientKey          string
	ServerName         string
	Durable            bool
	SchemasPath        string
}

// InfoError struct for sending detailed error messages to analysis.
// The empty interface allows for appending various json msgs but also broken json msgs as strings.
// It is ok as long as we do not need to access fields in the msg, which we don't.
type InfoError struct {
	Error           string      `json:"error"`
	Reason          string      `json:"reason"`
	OriginalMessage interface{} `json:"original-message"`
}

// NewMQ creates a new Broker that can communicate with a backend
// amqp server.
func NewMQ(config MQConf) (*AMQPBroker, error) {
	brokerURI := buildMQURI(config.Host, config.User, config.Password, config.Vhost, config.Port, config.Ssl)

	var Connection *amqp.Connection
	var Channel *amqp.Channel
	var err error

	log.Debugf("Connecting to broker host: %s:%d vhost: %s with user: %s", config.Host, config.Port, config.Vhost, config.User)
	if config.Ssl {
		var tlsConfig *tls.Config
		tlsConfig, err = TLSConfigBroker(config)
		if err != nil {
			return nil, err
		}
		Connection, err = amqp.DialTLS(brokerURI, tlsConfig)
	} else {
		Connection, err = amqp.Dial(brokerURI)
	}
	if err != nil {
		return nil, err
	}

	Channel, err = Connection.Channel()
	if err != nil {
		return nil, err
	}
	if config.Queue != "" {
		// The queues already exists so we can safely do a passive declaration
		_, err = Channel.QueueDeclarePassive(
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

	if e := Channel.Confirm(false); e != nil {
		fmt.Printf("channel could not be put into confirm mode: %s", e)

		return nil, fmt.Errorf("channel could not be put into confirm mode: %s", e)
	}

	confirms := Channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return &AMQPBroker{Connection, Channel, config, confirms}, nil
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
func (broker *AMQPBroker) SendMessage(corrID, exchange, routingKey string, reliable bool, body []byte) error {
	err := broker.Channel.Publish(
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

// buildMQURI builds the MQ connection URI
func buildMQURI(mqHost, mqUser, mqPassword, mqVhost string, mqPort int, ssl bool) string {
	brokerURI := ""
	if ssl {
		brokerURI = fmt.Sprintf("amqps://%s:%s@%s:%d%s", mqUser, mqPassword, mqHost, mqPort, mqVhost)
	} else {
		brokerURI = fmt.Sprintf("amqp://%s:%s@%s:%d%s", mqUser, mqPassword, mqHost, mqPort, mqVhost)
	}

	return brokerURI
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
		cacert, err := os.ReadFile(cacert) // #nosec this file comes from our config
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
	//nolint:nestif
	if config.VerifyPeer {
		if config.ClientCert != "" && config.ClientKey != "" {
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
		} else {
			logFatalf("No certificates supplied")
		}
	}
	if config.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}

	return &tlsConfig, nil
}

// ConnectionWatcher listens to events from the server
func (broker *AMQPBroker) ConnectionWatcher() *amqp.Error {
	amqpError := <-broker.Connection.NotifyClose(make(chan *amqp.Error))

	return amqpError
}

func (broker *AMQPBroker) ChannelWatcher() *amqp.Error {
	amqpError := <-broker.Channel.NotifyClose(make(chan *amqp.Error))

	return amqpError
}

// SendJSONError sends message on JSON error
func (broker *AMQPBroker) SendJSONError(delivered *amqp.Delivery, originalBody []byte, conf MQConf, reason, errorMsg string) error {

	jsonErrorMessage := InfoError{
		Error:           errorMsg,
		Reason:          fmt.Sprintf("%v", reason),
		OriginalMessage: string(originalBody),
	}

	body, _ := json.Marshal(jsonErrorMessage)

	return broker.SendMessage(delivered.CorrelationId, conf.Exchange, conf.RoutingError, conf.Durable, body)
}

// ValidateJSON validates JSON in body, verifying that it's valid JSON as well
// as conforming to the schema specified by messageType. It also optionally
// verifies that the message can be decoded into the supplied data structure dest
func (broker *AMQPBroker) ValidateJSON(delivered *amqp.Delivery,
	messageType string,
	body []byte,
	dest interface{}) error {
	res, err := validateJSON(messageType, broker.Conf.SchemasPath, body)

	if err != nil {
		log.Errorf("JSON error while validating "+
			"(corr-id: %s, error: %v, message body: %s)",
			delivered.CorrelationId,
			err,
			body)

		// Nack message so the server gets notified that something is wrong but don't requeue the message
		if e := delivered.Nack(false, false); e != nil {
			log.Errorf("Failed to Nack message "+
				"(corr-id: %s, errror: %v) ",
				delivered.CorrelationId,
				e)
		}
		// Send the message to an error queue so it can be analyzed.
		if e := broker.SendJSONError(delivered, body, broker.Conf, err.Error(), "Validation of JSON message failed"); e != nil {
			log.Errorf("Failed to publish JSON decode error message "+
				"(corr-id: %s, error: %v)",
				delivered.CorrelationId,
				e)
		}

		// Return error to restart on new message
		return err
	}

	if !res.Valid() {

		errorString := ""

		for _, validErr := range res.Errors() {
			errorString += validErr.String() + "\n\n"
		}

		log.Errorf("Error(s) while schema validation "+
			"(corr-id: %s, error: %s)",
			delivered.CorrelationId,
			errorString)
		log.Error("Validation failed")
		// Nack message so the server gets notified that something is wrong but don't requeue the message
		if e := delivered.Nack(false, false); e != nil {
			log.Errorf("Failed to Nack message "+
				"(corr-id: %s, error: %v)",
				delivered.CorrelationId,
				e)
		}
		// Send the message to an error queue so it can be analyzed.
		if e := broker.SendJSONError(delivered, body, broker.Conf, errorString, "Validation of JSON message failed"); e != nil {
			log.Errorf("Failed to publish JSON validity error message "+
				"(corr-id: %s, error: %v)",
				delivered.CorrelationId,
				e)

		}
		// Return error to restart on new message

		return fmt.Errorf("Errors while validating JSON %s", errorString)
	}

	if dest == nil {
		// Skip unmarshalling test
		return nil
	}
	//
	d := json.NewDecoder(bytes.NewBuffer(body))
	d.DisallowUnknownFields()
	err = d.Decode(dest)

	if err != nil {

		log.Errorf("Error while unmarshalling JSON "+
			"(corr-id: %s, error: %v, message body: %s)",
			delivered.CorrelationId,
			err,
			body)
	}

	return err
}

// validateJSON is a helper function for ValidateJson
func validateJSON(messageType string, schemasPath string, body []byte) (*gojsonschema.Result, error) {

	schema := gojsonschema.NewReferenceLoader(schemasPath + "/" + messageType + ".json")
	res, err := gojsonschema.Validate(schema, gojsonschema.NewBytesLoader(body))

	return res, err
}
