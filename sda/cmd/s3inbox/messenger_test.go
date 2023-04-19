package main

import (
	"crypto/tls"
	"encoding/json"
	"os"
	"testing"

	helper "sensitive-data-archive/internal/helper"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type MessengerTestSuite struct {
	suite.Suite
}

func (suite *MessengerTestSuite) SetupTest() {
	certPath, _ = os.MkdirTemp("", "gocerts")
	helper.MakeCerts(certPath)

	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", MQport)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.routingkey", "ingest")
	viper.Set("broker.exchange", "sda")
	viper.Set("broker.vhost", "sda")
	viper.Set("aws.url", "testurl")
	viper.Set("aws.accesskey", "testaccess")
	viper.Set("aws.secretkey", "testsecret")
	viper.Set("aws.bucket", "testbucket")
	viper.Set("server.jwtpubkeypath", "testpath")
}
func TestMessengerTestSuite(t *testing.T) {
	suite.Run(t, new(MessengerTestSuite))
}

func (suite *MessengerTestSuite) TestBuildMqURI() {
	amqps := buildMqURI("localhost", "5555", "mquser", "mqpass", "/vhost", true)
	assert.Equal(suite.T(), "amqps://mquser:mqpass@localhost:5555/vhost", amqps)
	amqp := buildMqURI("localhost", "5555", "mquser", "mqpass", "/vhost", false)
	assert.Equal(suite.T(), "amqp://mquser:mqpass@localhost:5555/vhost", amqp)
}

func (suite *MessengerTestSuite) TestNewAMQPMessenger() {
	config, err := NewConfig()
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), config)
	tlsConfig := new(tls.Config)

	assert.NotNil(suite.T(), tlsConfig)
	assert.NoError(suite.T(), err)
	m, err := NewAMQPMessenger(config.Broker, tlsConfig)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), m)
}

func (suite *MessengerTestSuite) TestSendMessage() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsConfig, err := TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsConfig)
	assert.NoError(suite.T(), err)

	messenger, err := NewAMQPMessenger(config.Broker, tlsConfig)
	assert.NoError(suite.T(), err)
	event := Event{}
	checksum := Checksum{}
	event.Operation = "TestSendMessage"
	event.Username = "Dummy"
	checksum.Type = "md5"
	checksum.Value = "123456789"
	event.Checksum = []interface{}{checksum}

	jsonMessage, err := json.Marshal(event)
	assert.NoError(suite.T(), err)
	uuid, _ := uuid.NewRandom()
	suite.T().Log("uuid: ", uuid)
	err = messenger.SendMessage(uuid.String(), jsonMessage)
	assert.NoError(suite.T(), err)
}

func (suite *MessengerTestSuite) TestCreateNewChannel() {
	config, err := NewConfig()
	assert.NotNil(suite.T(), config)
	assert.NoError(suite.T(), err)
	tlsConfig, err := TLSConfigBroker(config)
	assert.NotNil(suite.T(), tlsConfig)
	assert.NoError(suite.T(), err)

	messenger, err := NewAMQPMessenger(config.Broker, tlsConfig)
	messenger.channel.Close()
	assert.NoError(suite.T(), err)
	event := Event{}
	checksum := Checksum{}
	event.Operation = "TestRecreateChannel"
	event.Username = "Dummy"
	checksum.Type = "md5"
	checksum.Value = "123456789"
	event.Checksum = []interface{}{checksum}

	jsonMessage, err := json.Marshal(event)
	assert.NoError(suite.T(), err)
	uuid, _ := uuid.NewRandom()
	suite.T().Log("uuid: ", uuid)
	err = messenger.SendMessage(uuid.String(), jsonMessage)
	assert.NoError(suite.T(), err)
}
