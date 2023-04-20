package broker

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"os"
	"testing"

	"sensitive-data-archive/internal/helper"

	log "github.com/sirupsen/logrus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type BrokerTestSuite struct {
	suite.Suite
}

var certPath string
var tMqconf = MQConf{}

func (suite *BrokerTestSuite) SetupTest() {
	certPath, _ = os.MkdirTemp("", "gocerts")
	// defer os.RemoveAll(certPath)
	helper.MakeCerts(certPath)

	tMqconf = MQConf{
		"127.0.0.1",
		5678,
		"guest",
		"guest",
		"/",
		"ingest",
		"amq.default",
		"ingest",
		"error",
		false,
		false,
		certPath + "/ca.crt",
		certPath + "/tls.crt",
		certPath + "/tls.key",
		"mq",
		true,
		"",
	}
}

func (suite *BrokerTestSuite) TearDownTest() {
	defer os.RemoveAll(certPath)
}

func TestBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(BrokerTestSuite))
}

func (suite *BrokerTestSuite) TestBuildMqURI() {
	amqps := buildMQURI("localhost", "user", "pass", "/vhost", 5555, true)
	assert.Equal(suite.T(), "amqps://user:pass@localhost:5555/vhost", amqps)
	amqp := buildMQURI("localhost", "user", "pass", "/vhost", 5555, false)
	assert.Equal(suite.T(), "amqp://user:pass@localhost:5555/vhost", amqp)
}

func (suite *BrokerTestSuite) TestTLSConfigBroker() {
	confOK := tMqconf
	confOK.Ssl = true
	confOK.VerifyPeer = true
	confOK.CACert = certPath + "/ca.crt"
	confOK.ClientCert = certPath + "/tls.crt"
	confOK.ClientKey = certPath + "/tls.key"

	tlsConfig, err := TLSConfigBroker(confOK)
	assert.NoError(suite.T(), err, "Unexpected error")
	assert.NotZero(suite.T(), tlsConfig.Certificates, "Expected warnings were missing")
	assert.NotZero(suite.T(), tlsConfig.RootCAs, "Expected warnings were missing")
	assert.EqualValues(suite.T(), tlsConfig.ServerName, "mq")

	noCa := confOK
	noCa.CACert = ""
	_, err = TLSConfigBroker(noCa)
	assert.NoError(suite.T(), err, "Unexpected error")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	noCa.CACert = certPath + "/tls.key"
	_, err = TLSConfigBroker(noCa)
	assert.NoError(suite.T(), err, "Unexpected error")
	assert.Contains(suite.T(), buf.String(), "No certs appended, using system certs only")

	badCertConf := confOK
	badCertConf.ClientCert = certPath + "/bar"
	_, err = CatchTLSConfigBrokerPanic(badCertConf)
	assert.EqualError(suite.T(), err, "open "+certPath+"/bar: no such file or directory")

	badKeyConf := confOK
	badKeyConf.ClientKey = certPath + "/foo"
	_, err = CatchTLSConfigBrokerPanic(badKeyConf)
	assert.EqualError(suite.T(), err, "open "+certPath+"/foo: no such file or directory")

	noPemFile := confOK
	noPemFile.ClientKey = "broker.go"
	_, err = CatchTLSConfigBrokerPanic(noPemFile)
	assert.EqualError(suite.T(), err, "tls: failed to find any PEM data in key input")
}

func CatchTLSConfigBrokerPanic(c MQConf) (cfg *tls.Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("was panic, recovered value: %v", r)
		}
	}()

	cfg, err = TLSConfigBroker(c)

	return cfg, err
}

func (suite *BrokerTestSuite) TestNewMQNoTLS() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestNewMQTLS() {
	SslConf := tMqconf
	SslConf.Port = 5679
	SslConf.VerifyPeer = true

	b, err := NewMQ(SslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestSendMessage() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	err = b.SendMessage("1", "", "queue", true, []byte("test message"))
	assert.NoError(suite.T(), err)

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestGetMessages() {
	noSslConf := tMqconf
	noSslConf.Ssl = false
	b, err := NewMQ(noSslConf)
	if err != nil {
		suite.T().Log(err)
		suite.T().Skip("skip test since a real MQ is not present")
	}
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	d, err := b.GetMessages("queue")
	assert.NoError(suite.T(), err)

	for message := range d {
		if "test message" == string(message.Body) {
			err := message.Ack(false)
			assert.NoError(suite.T(), err)

			break
		}
	}

	b.Channel.Close()
	b.Connection.Close()
}
