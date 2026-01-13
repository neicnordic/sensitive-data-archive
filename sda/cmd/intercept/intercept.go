// The intercept service relays message between the queue
// provided from the federated service and local queues.
package main

import (
	"encoding/json"
	"errors"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"

	log "github.com/sirupsen/logrus"
)

const (
	msgAccession string = "accession"
	msgCancel    string = "cancel"
	msgIngest    string = "ingest"
	msgMapping   string = "mapping"
	msgRelease   string = "release"
	msgDeprecate string = "deprecate"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("intercept")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := mq.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("Starting intercept service")

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for delivered := range messages {
			log.Debugf("Received a message: %s", delivered.Body)

			msgType, err := typeFromMessage(delivered.Body)
			if err != nil {
				log.Errorf("Failed to get type for message (%v), reason: %v", msgType, err.Error())
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: (%v)", err)
				}
				// Restart on new message
				continue
			}

			routing := map[string]string{
				msgAccession: "accession",
				msgCancel:    "ingest",
				msgIngest:    "ingest",
				msgMapping:   "mappings",
				msgRelease:   "mappings",
				msgDeprecate: "mappings",
			}

			routingKey := routing[msgType]

			if routingKey == "" {
				log.Debugf("msg type: %s", msgType)
				if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, "undeliverable", delivered.Body); err != nil {
					log.Errorf("failed to publish message, reason: (%v)", err)
				}
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message for reason: %v", err)
				}

				continue
			}

			log.Infof("Routing message (correlation-id: %s, routingkey: %s)", delivered.CorrelationId, routingKey)
			if err := mq.SendMessage(delivered.CorrelationId, conf.Broker.Exchange, routingKey, delivered.Body); err != nil {
				log.Errorf("failed to publish message, reason: (%v)", err)
			}
			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to ack message for reason: %v", err)
			}
		}
	}()

	<-forever
}

// typeFromMessage returns the type value given a JSON structure for the message
// supplied in body
func typeFromMessage(body []byte) (string, error) {
	message := make(map[string]any)
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	msgTypeFetch, ok := message["type"]
	if !ok {
		return "", errors.New("malformed message, type is missing")
	}

	msgType, ok := msgTypeFetch.(string)
	if !ok {
		return "", errors.New("could not cast type attribute to string")
	}

	return msgType, nil
}
