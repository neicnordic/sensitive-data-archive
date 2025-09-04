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
	helper.MakeCerts(certPath)
	_ = writeConf(certPath)

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

	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(rabbitmq); err != nil {
		log.Panicf("Could not purge resource: %s", err)
	}

	os.RemoveAll(certPath)
	os.Exit(code)
}

func (ts *BrokerTestSuite) SetupTest() {
	tMqconf = MQConf{
		"127.0.0.1",
		mqPort,
		"guest",
		"guest",
		"/",
		"ingest",
		"amq.default",
		"ingest",
		false,
		false,
		certPath + "/ca.crt",
		certPath + "/tls.crt",
		certPath + "/tls.key",
		"mq",
		"",
		2,
	}
}

func TestBrokerTestSuite(t *testing.T) {
	suite.Run(t, new(BrokerTestSuite))
}

func (ts *BrokerTestSuite) TestBuildMqURI() {
	amqpsURL := buildMQURI("localhost", "user", "pass", "/vhost", 5555, true)
	assert.Equal(ts.T(), "amqps://user:pass@localhost:5555/vhost", amqpsURL)
	amqpURL := buildMQURI("localhost", "user", "pass", "/vhost", 5555, false)
	assert.Equal(ts.T(), "amqp://user:pass@localhost:5555/vhost", amqpURL)
}

func (ts *BrokerTestSuite) TestTLSConfigBroker() {
	confOK := tMqconf
	confOK.Ssl = true
	confOK.VerifyPeer = true
	confOK.CACert = certPath + "/ca.crt"
	confOK.ClientCert = certPath + "/tls.crt"
	confOK.ClientKey = certPath + "/tls.key"

	tlsConfig, err := TLSConfigBroker(confOK)
	assert.NoError(ts.T(), err, "Unexpected error")
	assert.NotZero(ts.T(), tlsConfig.Certificates, "Expected warnings were missing")
	assert.NotZero(ts.T(), tlsConfig.RootCAs, "Expected warnings were missing")
	assert.EqualValues(ts.T(), tlsConfig.ServerName, "mq")

	noCa := confOK
	noCa.CACert = ""
	_, err = TLSConfigBroker(noCa)
	assert.NoError(ts.T(), err, "Unexpected error")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	noCa.CACert = certPath + "/tls.key"
	_, err = TLSConfigBroker(noCa)
	assert.NoError(ts.T(), err, "Unexpected error")
	assert.Contains(ts.T(), buf.String(), "No certs appended, using system certs only")

	badCertConf := confOK
	badCertConf.ClientCert = certPath + "/bar"
	_, err = CatchTLSConfigBrokerPanic(badCertConf)
	assert.EqualError(ts.T(), err, "open "+certPath+"/bar: no such file or directory")

	badKeyConf := confOK
	badKeyConf.ClientKey = certPath + "/foo"
	_, err = CatchTLSConfigBrokerPanic(badKeyConf)
	assert.EqualError(ts.T(), err, "open "+certPath+"/foo: no such file or directory")

	noPemFile := confOK
	noPemFile.ClientKey = "broker.go"
	_, err = CatchTLSConfigBrokerPanic(noPemFile)
	assert.EqualError(ts.T(), err, "tls: failed to find any PEM data in key input")
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

func (ts *BrokerTestSuite) TestNewMQNoTLS() {
	b, err := NewMQ(tMqconf)
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(ts.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (ts *BrokerTestSuite) TestNewMQTLS() {
	sslConf := tMqconf
	sslConf.Port = tlsPort
	sslConf.Ssl = true
	sslConf.VerifyPeer = true

	b, err := NewMQ(sslConf)
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(ts.T(), b.Connection.IsClosed())

	b.Channel.Close()
	b.Connection.Close()
}

func (ts *BrokerTestSuite) TestSendMessage() {
	b, err := NewMQ(tMqconf)
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(ts.T(), b.Connection.IsClosed())

	err = b.SendMessage(context.TODO(), "1", "", "ingest", []byte("test message"))
	assert.NoError(ts.T(), err)

	b.Channel.Close()
	b.Connection.Close()
}

func (ts *BrokerTestSuite) TestGetMessages() {
	b, err := NewMQ(tMqconf)
	assert.NoError(ts.T(), err)
	assert.NotNil(ts.T(), b, "NewMQ without ssl did not return a broker")
	assert.False(ts.T(), b.Connection.IsClosed())

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
	assert.NoError(ts.T(), err)

	d, err := b.GetMessages(context.TODO(), "ingest")
	assert.NoError(ts.T(), err)

	for message := range d {
		if string(message.Message.Body) == "test message" {
			err := message.Message.Ack(false)
			assert.NoError(ts.T(), err)

			break
		}
	}

	b.Channel.Close()
	b.Connection.Close()
}

func (ts *BrokerTestSuite) TestCreateNewChannel() {
	b, err := NewMQ(tMqconf)
	assert.NoError(ts.T(), err)
	assert.False(ts.T(), b.Channel.IsClosed())

	b.Channel.Close()
	assert.True(ts.T(), b.Channel.IsClosed())

	assert.NoError(ts.T(), b.CreateNewChannel())
	assert.False(ts.T(), b.Channel.IsClosed())
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
