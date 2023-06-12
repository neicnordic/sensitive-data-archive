package broker

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const doesNotExist = "/does/not/exist"
const readableFile = "broker.go"

func TestMain(m *testing.M) {
	logFatalf = testLogFatalf
	code := m.Run()

	os.Exit(code)
}

func testLogFatalf(f string, args ...interface{}) {
	s := fmt.Sprintf(f, args...)
	panic(s)
}

type mockChannel struct {
	failConfirm    bool
	failPublish    bool
	confirmChannel chan amqp.Confirmation
}

func (c *mockChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	return nil, fmt.Errorf("error")
}

func (c *mockChannel) QueueDeclarePassive(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{}, fmt.Errorf("error")
}

func (c *mockChannel) Confirm(noWait bool) error {
	if c.failConfirm {
		return fmt.Errorf("failing for testing")
	}

	return nil
}

func (c *mockChannel) NotifyClose(ch chan *amqp.Error) chan *amqp.Error {
	close(ch)

	return ch
}

func (c *mockChannel) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {

	c.confirmChannel = confirm

	return confirm
}

func (c *mockChannel) Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {

	if c.failPublish {
		return fmt.Errorf("failPublish")
	}

	ack := amqp.Confirmation{}
	ack.DeliveryTag = 1
	ack.Ack = true
	c.confirmChannel <- ack

	return nil
}

func (*mockChannel) Close() error {
	return nil
}

func (*mockChannel) IsClosed() bool {
	return false
}

func TestGetMessages_Error(t *testing.T) {
	b := AMQPBroker{}
	c := mockChannel{}
	b.Channel = &c

	var str bytes.Buffer
	log.SetOutput(&str)

	_, err := b.GetMessages("queue")
	assert.Error(t, err, "Must be an error")
}

func TestSendMessage(t *testing.T) {
	b := AMQPBroker{}
	c := mockChannel{}

	var err error
	b.Channel = &c
	err = b.Channel.Confirm(false)
	assert.Nil(t, err, "Could not Channel in confirm mode")
	b.confirmsChan = b.Channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	msg := []byte("Message")

	err = b.SendMessage("corrID1", "exchange", "routingkey", false, msg)
	assert.Nil(t, err, "Unexpected error from SendMessage (not reliable)")

	err = b.SendMessage("corrID1", "exchange", "routingkey", true, msg)
	assert.Nil(t, err, "Unexpected error from SendMessage (reliable)")

}

var tMqconf = MQConf{
	"127.0.0.1",
	6565,
	"user",
	"password",
	"/vhost",
	"queue",
	"exchange",
	"routingkey",
	"routingError",
	true,
	false,
	true,
	"../../dev_utils/certs/ca.pem",
	"../../dev_utils/certs/client.pem",
	"../../dev_utils/certs/client-key.pem",
	"servername",
	true,
	"file://../../schemas/federated/",
	2,
}

func TestBuildMqURI(t *testing.T) {
	amqps := buildMQURI("localhost", "user", "pass", "/vhost", 5555, true)
	assert.Equal(t, "amqps://user:pass@localhost:5555/vhost", amqps)
	amqp := buildMQURI("localhost", "user", "pass", "/vhost", 5555, false)
	assert.Equal(t, "amqp://user:pass@localhost:5555/vhost", amqp)
}

func TestNewMQ(t *testing.T) {
	noSSLPort := 5555 // + int(rand.Float64()*32768)
	s := startServer(t, noSSLPort)

	noSslConf := tMqconf
	noSslConf.Ssl = false
	noSslConf.VerifyPeer = false
	noSslConf.Port = noSSLPort

	go handleOneConnection(s.Sessions, false, false)
	b, e := NewMQ(noSslConf)
	assert.Nil(t, e, "Unwanted Error")
	assert.NotNil(t, b, "NewMQ without ssl did not return a broker")
	// Fail the queuedeclarepassive
	go handleOneConnection(s.Sessions, false, true)
	errret := CatchNewMQPanic(t, noSslConf)
	assert.NotNil(t, errret, "NewMQ did fail as expected")
	// Fail the channel
	go handleOneConnection(s.Sessions, true, true)
	errret = CatchNewMQPanic(t, noSslConf)
	assert.NotNil(t, errret, "NewMQ did fail as expected")
	s.Close()
}

func CatchNewMQPanic(t *testing.T, conf MQConf) (err error) {
	// Recover if NewMQ panics
	// Allow both panic and error return here, so use a custom function rather
	// than assert.Panics

	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("Caught panic")
		}
	}()

	b, _ := NewMQ(conf)

	if b == nil {
		return fmt.Errorf("NewMQ did not return a broker")
	}

	return nil
}

// Test connection refused as there is no running
// broker
func TestNewMQConn_Error(t *testing.T) {

	brokerURI := buildMQURI(tMqconf.Host, tMqconf.User, tMqconf.Password, tMqconf.Vhost, tMqconf.Port, tMqconf.Ssl)

	expectedMsg := "dial tcp 127.0.0.1:6565: connect: connection refused"

	_, err := amqp.Dial(brokerURI)

	assert.EqualError(t, err, expectedMsg)

	noSslConf := tMqconf
	noSslConf.Ssl = false
	noSslConf.VerifyPeer = false
	noSslConf.Port = 42

	newErr := CatchNewMQPanic(t, noSslConf)

	if newErr == nil {
		t.Errorf("New MQ did not report error when it should.")
	}
}

func TestNewMQTLS(t *testing.T) {
	if _, err := os.Stat("../../dev_utils/certs/ca.pem"); err != nil {
		t.Skip("skip test since certificates are not present")
	}
	sslPort := 5556
	sslConf := tMqconf
	sslConf.Ssl = true
	sslConf.VerifyPeer = false
	sslConf.Port = sslPort
	sslConf.ServerName = sslConf.Host

	serverTLSConfig := tlsServerConfig()
	serverTLSConfig.ClientAuth = tls.NoClientCert

	ss := startTLSServer(t, sslPort, serverTLSConfig)
	go handleOneConnection(ss.Sessions, false, false)

	b, e := NewMQ(sslConf)
	assert.Nil(t, e, "Unwanted Error")
	assert.NotNil(t, b, "NewMQ with ssl did not return a broker")

	ss.Close()
}

func CatchTLSConfigBrokerPanic(b MQConf) (cfg *tls.Config, err error) {
	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("Caught panic")
		}
	}()

	cfg, _ = TLSConfigBroker(b)

	return cfg, nil
}

func TestTLSConfigBroker(t *testing.T) {
	tlsConfig, err := TLSConfigBroker(tMqconf)
	if err != nil && strings.Contains(err.Error(), "no such file or directory") {
		t.Skip("skip test since certificates are not present")
	}
	assert.NoError(t, err, "Unexpected error")
	assert.NotZero(t, tlsConfig.Certificates, "Expected warnings were missing")
	assert.EqualValues(t, tlsConfig.ServerName, "servername")

	noSslConf := tMqconf
	noSslConf.Ssl = false
	noSslConf.VerifyPeer = false
	noSslConf.CACert = ""
	noSslConf.ClientCert = ""
	noSslConf.ClientKey = ""

	notls, err := TLSConfigBroker(noSslConf)
	assert.NoError(t, err, "Unexpected error")
	assert.Zero(t, notls.Certificates, "Expected warnings were missing")

	sslConf := noSslConf
	sslConf.CACert = doesNotExist

	sslConf.CACert = readableFile

	_, _ = CatchTLSConfigBrokerPanic(sslConf)
	// Should we fail here?
	//	assert.NotNil(t, err, "Expected failure was missing")

	sslConf.VerifyPeer = true
	sslConf.CACert = ""
	sslConf.ClientKey = doesNotExist
	sslConf.ClientCert = doesNotExist

	_, _ = CatchTLSConfigBrokerPanic(sslConf)
	// Should we fail here?
	//	assert.NotNil(t, err, "Expected failure was missing")

	sslConf.VerifyPeer = true
	sslConf.CACert = ""
	sslConf.ClientKey = doesNotExist
	sslConf.ClientCert = readableFile

	_, _ = CatchTLSConfigBrokerPanic(sslConf)
	// Should we fail here?
	//	assert.NotNil(t, err, "Expected failure was missing")

	sslConf.CACert = ""
	sslConf.ClientKey = ""
	sslConf.ClientCert = ""

	_, _ = CatchTLSConfigBrokerPanic(sslConf)
	// Should we fail here?
	//	assert.NotNil(t, err, "Expected failure was missing")

}

func TestValidateJSON(t *testing.T) {
	b := AMQPBroker{}
	c := mockChannel{}

	b.Channel = &c
	b.Conf = tMqconf

	type checksums struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}

	type finalize struct {
		Type               string      `json:"type"`
		User               string      `json:"user"`
		Filepath           string      `json:"filepath"`
		AccessionID        string      `json:"accession_id"`
		DecryptedChecksums []checksums `json:"decrypted_checksums"`
	}

	type testStruct struct {
		Test string `json:"test"`
	}

	msg := amqp.Delivery{CorrelationId: "2"}
	message := finalize{
		Type:        "accession",
		User:        "foo",
		Filepath:    "dummy_data.c4gh",
		AccessionID: "EGAF12345678901",
		DecryptedChecksums: []checksums{
			{"sha256", "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	messageText, _ := json.Marshal(message)
	decoded := finalize{}
	c.failPublish = true

	var buf bytes.Buffer

	log.SetOutput(&buf)
	err := b.ValidateJSON(&msg, "ingestion-accession", messageText, &decoded)
	assert.Nil(t, err, "ValidateJSON failed unexpectedly")
	assert.Zero(t, buf.Len(), "Logs from correct validation")

	err = b.ValidateJSON(&msg, "ingestion-accession", messageText, nil)
	assert.Nil(t, err, "ValidateJSON failed unexpectedly")
	assert.Zero(t, buf.Len(), "Logs from correct validation")

	err = b.ValidateJSON(&msg, "ingestion-accession", messageText[:20], &decoded)
	assert.Error(t, err, "ValidateJSON did not fail when it should")
	assert.True(t, strings.Contains(buf.String(), "JSON decode error"),
		"Did not see expected log from failed validation")

	buf.Reset()
	err = b.ValidateJSON(&msg, "ingestion-accession", messageText, new(testStruct))
	assert.Error(t, err, "ValidateJSON did not fail when it should")
	assert.NotZero(t, buf.Len(), "Did not get expected logs from failed ValidateJSON")

	buf.Reset()

	err = b.ValidateJSON(&msg, "ingestion-accession", []byte("{\"test\":\"valid_json\"}"), new(testStruct))
	assert.Error(t, err, "ValidateJSON did not fail when it should")
	assert.True(t, strings.Contains(buf.String(), "JSON validity error"),
		"Did not see expected log from failed validation")

	err = b.ValidateJSON(&msg, "notfound", messageText, &decoded)
	assert.Error(t, err, "ValidateJSON did not fail when it should")
	assert.NotZero(t, buf.Len(), "Did not get expected logs from failed ValidateJSON")
}

func TestSendJSONError(t *testing.T) {
	b := AMQPBroker{}
	c := mockChannel{}

	b.Channel = &c
	b.Conf = tMqconf
	b.confirmsChan = b.Channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	var err error
	type testMsg struct {
		Test string `json:"payload"`
	}

	msg := amqp.Delivery{CorrelationId: "1"}

	message := testMsg{Test: "some json"}
	messageText, _ := json.Marshal(message)
	err = b.SendJSONError(&msg, messageText, b.Conf, "some reason", "some error msg")
	assert.Nil(t, err, "SendJSONError failed unexpectedly (json payload)")

	messageText = []byte("some string")
	err = b.SendJSONError(&msg, messageText, b.Conf, "some reason", "some error msg")
	assert.Nil(t, err, "SendJSONError failed unexpectedly (string payload)")
}
