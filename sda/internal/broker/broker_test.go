package broker

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/helper"

	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type BrokerTestSuite struct {
	suite.Suite
}

var mqPort, tlsPort int
var certPath string
var tMqconf = MQConf{}

func TestMain(m *testing.M) {
	certPath, _ = os.MkdirTemp("", "gocerts")
	defer os.RemoveAll(certPath)
	helper.MakeCerts(certPath)
	_ = writeConf(certPath)

	defer func() {
		if r := recover(); r != nil {
			log.Infoln("Recovered")
		}
	}()
	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Panicf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Panicf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "rabbitmq",
		Tag:        "3-management",
		Mounts: []string{
			certPath + "/rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf",
			certPath + "/ca.crt:/etc/rabbitmq/ca.crt",
			certPath + "/tls.crt:/etc/rabbitmq/tls.crt",
			certPath + "/tls.key:/etc/rabbitmq/tls.key",
		},
		Name: "mq",
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Panicf("Could not start resource: %s", err)
	}

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	tlsPort, _ = strconv.Atoi(rabbitmq.GetPort("5671/tcp"))
	mqHostAndPort := rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPut, "http://"+mqHostAndPort+"/api/queues/%2F/ingest", http.NoBody)
	if err != nil {
		log.Panic(err)
	}
	req.SetBasicAuth("guest", "guest")

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(rabbitmq); err != nil {
			log.Panicf("Could not purge resource: %s", err)
		}
		log.Panicf("Could not connect to rabbitmq: %s", err)
	}

	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(rabbitmq); err != nil {
		log.Panicf("Could not purge resource: %s", err)
	}
}

func (suite *BrokerTestSuite) SetupTest() {
	tMqconf = MQConf{
		"127.0.0.1",
		mqPort,
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
		2,
	}
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
	b, err := NewMQ(tMqconf)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestNewMQTLS() {
	SslConf := tMqconf
	SslConf.Port = tlsPort
	SslConf.Ssl = true
	SslConf.VerifyPeer = true

	b, err := NewMQ(SslConf)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestSendMessage() {
	b, err := NewMQ(tMqconf)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	err = b.SendMessage("1", "", "ingest", true, []byte("test message"))
	assert.NoError(suite.T(), err)

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestGetMessages() {
	b, err := NewMQ(tMqconf)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(suite.T(), b.Connection.IsClosed())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = b.Channel.PublishWithContext(
		ctx,
		"",
		"ingest",
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         amqp.Table{},
			ContentEncoding: "UTF-8",
			ContentType:     "application/json",
			DeliveryMode:    amqp.Persistent, // 1=non-persistent, 2=persistent
			CorrelationId:   "getMessage",
			Priority:        0, // 0-9
			Body:            []byte("test message"),
		},
	)
	assert.NoError(suite.T(), err)

	d, err := b.GetMessages("ingest")
	assert.NoError(suite.T(), err)

	for message := range d {
		if string(message.Body) == "test message" {
			err := message.Ack(false)
			assert.NoError(suite.T(), err)

			break
		}
	}

	b.Channel.Close()
	b.Connection.Close()
}

func (suite *BrokerTestSuite) TestCreateNewChannel() {
	b, err := NewMQ(tMqconf)
	assert.NoError(suite.T(), err)
	assert.False(suite.T(), b.Channel.IsClosed())

	b.Channel.Close()
	assert.True(suite.T(), b.Channel.IsClosed())

	assert.NoError(suite.T(), b.CreateNewChannel())
	assert.False(suite.T(), b.Channel.IsClosed())

}

// Helper functions below this line

func writeConf(dest string) error {
	f, err := os.Create(dest + "/rabbitmq.conf")
	if err != nil {
		return err
	}
	defer f.Close()

	conf := []byte("listeners.ssl.default  = 5671\n" +
		"ssl_options.cacertfile           = /etc/rabbitmq/ca.crt\n" +
		"ssl_options.certfile             = /etc/rabbitmq/tls.crt\n" +
		"ssl_options.keyfile              = /etc/rabbitmq/tls.key\n" +
		"ssl_options.verify               = verify_peer\n" +
		"ssl_options.fail_if_no_peer_cert = true\n",
	)

	_, err = f.Write(conf)
	if err != nil {
		return err
	}

	return nil
}
