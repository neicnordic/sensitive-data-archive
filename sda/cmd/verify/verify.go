// The verify service reads and decrypts ingested files from the archive
// storage and sends accession requests.
package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	log "github.com/sirupsen/logrus"
)

var (
	archive        storage.Backend
	mq             *broker.AMQPBroker
	conf           *config.Config
	db             *database.SDAdb
	archiveKeyList []*[32]byte
	message        schema.IngestionVerification
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, err := observability.SetupOTelSDK(ctx, "verify")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		if err := otelShutdown(ctx); err != nil {
			log.Errorf("failed to shutdown otel: %v", err)
		}
	}()

	ctx, span := observability.GetTracer().Start(ctx, "startUp")

	forever := make(chan bool)
	conf, err = config.NewConfig("verify")
	if err != nil {
		log.Fatal(err)
	}
	mq, err = broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err = database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	archive, err = storage.NewBackend(ctx, conf.Archive)
	if err != nil {
		log.Fatal(err)
	}
	archiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil || len(archiveKeyList) == 0 {
		log.Fatal("no C4GH private keys configured")
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()
	defer db.Close()

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

	log.Info("starting verify service")
	span.End()

	go func() {
		messages, err := mq.GetMessages(ctx, conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get messages (error: %v)", err)
		}
		for msg := range messages {
			ctx, span := observability.GetTracer().Start(msg.Context(), "handleMessage", trace.WithAttributes(attribute.String("correlation-id", msg.Message.CorrelationId)))

			if err := handleMessage(ctx, msg.Message); err != nil {
				// TODO err handle
				span.End()
				log.Fatal(err)
			}

			span.End()
		}
	}()

	<-forever
}

func handleMessage(ctx context.Context, delivered amqp.Delivery) error {
	log.Debugf("received a message (corr-id: %s, message: %s)", delivered.CorrelationId, delivered.Body)
	err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", conf.Broker.SchemasPath), delivered.Body)
	if err != nil {
		log.Errorf("validation of incoming message (ingestion-verification) failed, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
		// Send the message to an error queue so it can be analyzed.
		infoErrorMessage := broker.InfoError{
			Error:           "Message validation failed",
			Reason:          err.Error(),
			OriginalMessage: message,
		}

		body, _ := json.Marshal(infoErrorMessage)
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: %v", err)
		}
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed to Ack message, reason: %v", err)
		}

		// Restart on new message
		return nil
	}
	// we unmarshal the message in the validation step so this is safe to do
	_ = json.Unmarshal(delivered.Body, &message)

	log.Infof(
		"Received work (corr-id: %s, filepath: %s, user: %s)",
		delivered.CorrelationId, message.FilePath, message.User,
	)

	// If the file has been canceled by the uploader, don't spend time working on it.
	status, err := db.GetFileStatus(ctx, delivered.CorrelationId)
	if err != nil {
		log.Errorf("failed to get file status, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
		// Send the message to an error queue so it can be analyzed.
		infoErrorMessage := broker.InfoError{
			Error:           "Getheader failed",
			Reason:          err.Error(),
			OriginalMessage: message,
		}

		body, _ := json.Marshal(infoErrorMessage)
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: (%s)", err.Error())
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: (%s)", err.Error())
		}

		return nil
	}
	if status == "disabled" {
		log.Infof("file with correlation-id: %s is disabled, stopping verification", delivered.CorrelationId)
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed acking canceled work, reason: (%s)", err.Error())
		}

		return nil
	}

	header, err := db.GetHeader(ctx, message.FileID)
	if err != nil {
		log.Errorf("GetHeader failed for file with ID: %v, reason: %v", message.FileID, err.Error())
		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed to nack following getheader error message")
		}
		// store full message info in case we want to fix the db entry and retry
		infoErrorMessage := broker.InfoError{
			Error:           "Getheader failed",
			Reason:          err.Error(),
			OriginalMessage: message,
		}

		body, _ := json.Marshal(infoErrorMessage)

		// Send the message to an error queue so it can be analyzed.
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: (%s)", err.Error())
		}

		return nil
	}

	var file database.FileInfo
	file.Size, err = archive.GetFileSize(ctx, message.ArchivePath, false)
	if err != nil { //nolint:nestif
		log.Errorf("Failed to get archived file size, file-id: %s, archive-path: %s, reason: (%s)", message.FileID, message.ArchivePath, err.Error())
		if strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "NoSuchKey:") || strings.Contains(err.Error(), "NotFound:") {
			jsonMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			if err := db.UpdateFileEventLog(ctx, message.FileID, "error", delivered.CorrelationId, "verify", string(jsonMsg), string(delivered.Body)); err != nil {
				log.Errorf("failed to set ingestion status for file from message, file-id: %v", message.FileID)
			}
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed to Ack message, reason: (%s)", err.Error())
		}

		// Send the message to an error queue so it can be analyzed.
		fileError := broker.InfoError{
			Error:           "Failed to get archived file size",
			Reason:          err.Error(),
			OriginalMessage: message,
		}
		body, _ := json.Marshal(fileError)
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: (%s)", err.Error())
		}

		return nil
	}

	archiveFileHash := sha256.New()
	f, err := archive.NewFileReader(ctx, message.ArchivePath)
	if err != nil {
		log.Errorf("Failed to open archived file, file-id: %s, reason: %v ", message.FileID, err.Error())
		// Send the message to an error queue so it can be analyzed.
		infoErrorMessage := broker.InfoError{
			Error:           "Failed to open archived file",
			Reason:          err.Error(),
			OriginalMessage: message,
		}

		body, _ := json.Marshal(infoErrorMessage)
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("failed to publish message, reason: (%s)", err.Error())
		}

		return nil
	}

	var key *[32]byte
	for _, k := range archiveKeyList {
		size, err := headers.EncryptedSegmentSize(header, *k)
		if (err == nil) && (size != 0) {
			key = k

			break
		}
	}

	if key == nil {
		log.Errorf("no matching key found for file, file-id: %s, archive-path: %s", message.FileID, message.ArchivePath)

		return nil
	}

	mr := io.MultiReader(bytes.NewReader(header), io.TeeReader(f, archiveFileHash))
	c4ghr, err := streaming.NewCrypt4GHReader(mr, *key, nil)
	if err != nil {
		log.Errorf("failed to open c4gh decryptor stream, file-id: %s, archive-path: %s, reason: %s", message.FileID, message.ArchivePath, err.Error())

		return nil
	}

	md5hash := md5.New()
	sha256hash := sha256.New()
	stream := io.TeeReader(c4ghr, md5hash)

	if file.DecryptedSize, err = io.Copy(sha256hash, stream); err != nil {
		log.Errorf("failed to copy decrypted data, file-id: %s, reason: (%s)", message.FileID, err.Error())

		// Send the message to an error queue so it can be analyzed.
		infoErrorMessage := broker.InfoError{
			Error:           "Failed to verify archived file",
			Reason:          err.Error(),
			OriginalMessage: message,
		}

		body, _ := json.Marshal(infoErrorMessage)
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
			log.Errorf("Failed to publish error message, reason: (%s)", err.Error())
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed to ack message, reason: (%s)", err.Error())
		}

		return nil
	}

	// At this point we should do checksum comparison

	file.ArchiveChecksum = fmt.Sprintf("%x", archiveFileHash.Sum(nil))
	file.DecryptedChecksum = fmt.Sprintf("%x", sha256hash.Sum(nil))

	switch {
	case message.ReVerify:
		decrypted, err := db.GetDecryptedChecksum(ctx, message.FileID)
		if err != nil {
			log.Errorf("failed to get unencrypted checksum for file, file-id: %s, reason: %s", message.FileID, err.Error())
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}

		if file.DecryptedChecksum != decrypted {
			log.Errorf("encrypted checksum don't match for file, file-id: %s", message.FileID)
			if err := db.UpdateFileEventLog(ctx, message.FileID, "error", delivered.CorrelationId, "verify", `{"error":"decrypted checksum don't match"}`, string(delivered.Body)); err != nil {
				log.Errorf("set status ready failed, file-id: %s, reason: (%v)", message.FileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				return nil
			}
			if err := delivered.Ack(false); err != nil {
				log.Errorf("Failed to ack message, reason: (%s)", err.Error())
			}

			return nil
		}

		if file.ArchiveChecksum != message.EncryptedChecksums[0].Value {
			log.Errorf("encrypted checksum mismatch for file, file-id: %s, filepath: %s, expected: %s, got: %s", message.FileID, message.FilePath, message.EncryptedChecksums[0].Value, file.ArchiveChecksum)
			if err := db.UpdateFileEventLog(ctx, message.FileID, "error", delivered.CorrelationId, "verify", `{"error":"encrypted checksum don't match"}`, string(delivered.Body)); err != nil {
				log.Errorf("set status ready failed, file-id: %s, reason: (%v)", message.FileID, err)
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%v)", err)
				}

				return nil
			}
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("Failed to ack message, reason: (%s)", err.Error())
		}

		return nil
	default:
		c := schema.IngestionAccessionRequest{
			User:     message.User,
			FilePath: message.FilePath,
			DecryptedChecksums: []schema.Checksums{
				{Type: "sha256", Value: fmt.Sprintf("%x", sha256hash.Sum(nil))},
				{Type: "md5", Value: fmt.Sprintf("%x", md5hash.Sum(nil))},
			},
		}

		verifiedMessage, _ := json.Marshal(&c)
		err = schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession-request.json", conf.Broker.SchemasPath), verifiedMessage)
		if err != nil {
			log.Errorf("Validation of outgoing (ingestion-accession-request) failed, file-id: %s, reason: (%s)", message.FileID, err.Error())

			// Logging is in ValidateJSON so just restart on new message
			return nil
		}
		status, err := db.GetFileStatus(ctx, delivered.CorrelationId)
		if err != nil {
			log.Errorf("failed to get file status, correlation-id: %s, reason: (%s)", delivered.CorrelationId, err.Error())
			// Send the message to an error queue so it can be analyzed.
			infoErrorMessage := broker.InfoError{
				Error:           "Getheader failed",
				Reason:          err.Error(),
				OriginalMessage: message,
			}

			body, _ := json.Marshal(infoErrorMessage)
			if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, "error", body); err != nil {
				log.Errorf("failed to publish message, reason: (%s)", err.Error())
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("Failed acking canceled work, reason: (%s)", err.Error())
			}

			return nil
		}

		if status == "disabled" {
			log.Infof("file with correlation-id: %s is disabled, stopping verification", delivered.CorrelationId)
			if err := delivered.Ack(false); err != nil {
				log.Errorf("Failed acking canceled work, reason: (%s)", err.Error())
			}

			return nil
		}

		fileInfo, err := db.GetFileInfo(ctx, message.FileID)
		if err != nil {
			log.Errorf("failed to get info for file, file-id: %s", message.FileID)
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}

		if fileInfo.DecryptedChecksum != fmt.Sprintf("%x", sha256hash.Sum(nil)) {
			if err := db.SetVerified(ctx, file, message.FileID); err != nil {
				log.Errorf("SetVerified failed, file-id: %s, reason: (%s)", message.FileID, err.Error())
				if err := delivered.Nack(false, true); err != nil {
					log.Errorf("failed to Nack message, reason: (%s)", err.Error())
				}

				return nil
			}
		} else {
			log.Infof("file is already verified, file-id: %s", message.FileID)
		}

		if err := db.UpdateFileEventLog(ctx, message.FileID, "verified", delivered.CorrelationId, "ingest", "{}", string(verifiedMessage)); err != nil {
			log.Errorf("failed to set event log status for file, file-id: %s", message.FileID)
			if err := delivered.Nack(false, true); err != nil {
				log.Errorf("failed to Nack message, reason: (%s)", err.Error())
			}

			return nil
		}

		// Send message to verified queue
		if err := mq.SendMessage(ctx, delivered.CorrelationId, conf.Broker.Exchange, conf.Broker.RoutingKey, verifiedMessage); err != nil {
			// TODO fix resend mechanism
			log.Errorf("failed to publish message, reason: (%s)", err.Error())

			return nil
		}

		if err := delivered.Ack(false); err != nil {
			log.Errorf("failed to Ack message, reason: (%s)", err.Error())
		}
	}
	log.Infof("Successfully verified the file, file-id: %s, filepath: %s", message.FileID, message.FilePath)
	return nil
}
