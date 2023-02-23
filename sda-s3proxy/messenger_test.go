package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestBuildMqURI(t *testing.T) {
	amqps := buildMqURI("localhost", "5555", "mquser", "mqpass", "/vhost", true)
	assert.Equal(t, "amqps://mquser:mqpass@localhost:5555/vhost", amqps)
	amqp := buildMqURI("localhost", "5555", "mquser", "mqpass", "/vhost", false)
	assert.Equal(t, "amqp://mquser:mqpass@localhost:5555/vhost", amqp)
}

func TestNewAMQPMessenger(t *testing.T) {
	viper.Reset()
	viper.Set("server.confFile", "dev_utils/config.yaml")

	config, err := NewConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)
	tlsConfig, err := TLSConfigBroker(config)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since certificates are not present")
	}
	assert.NotNil(t, tlsConfig)
	assert.NoError(t, err)
	m, err := NewAMQPMessenger(config.Broker, tlsConfig)
	assert.NoError(t, err)
	assert.NotNil(t, m)
}

func TestSendMessage(t *testing.T) {
	viper.Reset()
	viper.Set("server.confFile", "dev_utils/config.yaml")

	config, err := NewConfig()
	assert.NotNil(t, config)
	assert.NoError(t, err)
	tlsConfig, err := TLSConfigBroker(config)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since certificates are not present")
	}
	assert.NotNil(t, tlsConfig)
	assert.NoError(t, err)

	messenger, err := NewAMQPMessenger(config.Broker, tlsConfig)
	assert.NoError(t, err)
	event := Event{}
	checksum := Checksum{}
	event.Operation = "TestSendMessage"
	event.Username = "Dummy"
	checksum.Type = "md5"
	checksum.Value = "123456789"
	event.Checksum = []interface{}{checksum}

	jsonMessage, err := json.Marshal(event)
	assert.NoError(t, err)
	uuid, _ := uuid.NewRandom()
	t.Log("uuid: ", uuid)
	err = messenger.SendMessage(uuid.String(), jsonMessage)
	assert.NoError(t, err)
}

func TestCreateNewChannel(t *testing.T) {
	viper.Reset()
	viper.Set("server.confFile", "dev_utils/config.yaml")

	config, err := NewConfig()
	assert.NotNil(t, config)
	assert.NoError(t, err)
	tlsConfig, err := TLSConfigBroker(config)
	if err != nil {
		t.Log(err)
		t.Skip("skip test since certificates are not present")
	}
	assert.NotNil(t, tlsConfig)
	assert.NoError(t, err)

	messenger, err := NewAMQPMessenger(config.Broker, tlsConfig)
	messenger.channel.Close()
	assert.NoError(t, err)
	event := Event{}
	checksum := Checksum{}
	event.Operation = "TestRecreateChannel"
	event.Username = "Dummy"
	checksum.Type = "md5"
	checksum.Value = "123456789"
	event.Checksum = []interface{}{checksum}

	jsonMessage, err := json.Marshal(event)
	assert.NoError(t, err)
	uuid, _ := uuid.NewRandom()
	t.Log("uuid: ", uuid)
	err = messenger.SendMessage(uuid.String(), jsonMessage)
	assert.NoError(t, err)
}
