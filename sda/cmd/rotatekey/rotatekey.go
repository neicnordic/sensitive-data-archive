// The rotatekey service accepts messages to re-encrypt a file identified by its fileID.
// The service re-encrypts the file header with a configured public key and stores it
// in the database together with the key-hash of the rotation key.
// It then sends a message to verify so that the file is re-verified.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	rotatekeyconfig "github.com/neicnordic/sensitive-data-archive/cmd/rotatekey/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type rotateKey struct {
	broker                    broker.Broker
	db                        database.Database
	reverifyRoutingKey        string
	targetPublicKeyPemEncoded string
	schemaPath                string
	targetPublicKey           *[32]byte
	targetKeyNotUsableChan    chan error
	reencryptClient           reencrypt.ReencryptClient
	reencryptClientTimeout    time.Duration
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-rotatekey")
	if err != nil {
		panic(fmt.Errorf("failed to setup OTel SDK: %v", err))
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			slog.Error("failed to shutdown OTel SDK", "err", err)
		}
	}()

	ctx, startupSpan := observability.StartSpan(ctx, "start up")
	defer startupSpan.End()

	targetPublicKey, err := config.GetC4GHPublicKey(rotatekeyconfig.TargetPublicKey())
	if err != nil {
		return fmt.Errorf("failed to load target public key: %v", err)
	}

	app := &rotateKey{
		schemaPath:             rotatekeyconfig.SchemaPath(),
		targetKeyNotUsableChan: make(chan error, 1),
		reencryptClientTimeout: rotatekeyconfig.ReencryptTimeout(),
		reverifyRoutingKey:     rotatekeyconfig.RoutingKey(),
		targetPublicKey:        targetPublicKey,
	}

	var opts []grpc.DialOption
	switch {
	case rotatekeyconfig.ReencryptClientCert() != "" && rotatekeyconfig.ReencryptClientKey() != "":
		certs, err := tls.LoadX509KeyPair(rotatekeyconfig.ReencryptClientCert(), rotatekeyconfig.ReencryptClientKey())
		if err != nil {
			return fmt.Errorf("failed to load client key pair for reencrypt: %v", err)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{certs},
			MinVersion:   tls.VersionTLS13,
		}

		if rotatekeyconfig.ReencryptCaCert() != "" {
			caCertByte, err := os.ReadFile(rotatekeyconfig.ReencryptCaCert())
			if err != nil {
				return fmt.Errorf("failed to read ca certificate file:: %s", err.Error())
			}

			caCert := x509.NewCertPool()
			if !caCert.AppendCertsFromPEM(caCertByte) {
				return errors.New("failed to append CA certificate to cert pool")
			}
			tlsConfig.RootCAs = caCert
		}

		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	reencryptGrpcConn, err := grpc.NewClient(rotatekeyconfig.ReencryptTarget(), opts...)
	if err != nil {
		return fmt.Errorf("failed to create new grpc client: %w", err)
	}
	defer func() {
		if err := reencryptGrpcConn.Close(); err != nil {
			slog.Error("failed to close reencrypt grpc connection: %v", slog.Any("error", err))
		}
	}()

	app.reencryptClient = reencrypt.NewReencryptClient(reencryptGrpcConn)

	app.db, err = postgres.NewPostgresSQLDatabase(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer func() {
		if err := app.db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}
	}()

	if dbSchemaVersion, err := app.db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	app.broker, err = rabbitmq.NewRabbitMQBroker(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker: %v", err)
	}
	defer func() {
		if err := app.broker.Close(); err != nil {
			slog.Error("could not close broker", slog.Any("error", err))
		}
	}()

	// encode pubkey as pem and then as base64 string
	tmp := &bytes.Buffer{}
	if err := keys.WriteCrypt4GHX25519PublicKey(tmp, *app.targetPublicKey); err != nil {
		return fmt.Errorf("failed to encode public key to pem: %w", err)
	}
	app.targetPublicKeyPemEncoded = base64.StdEncoding.EncodeToString(tmp.Bytes())

	// Check that key is registered in the db at startup
	err = app.checkKeyHash(ctx, hex.EncodeToString(app.targetPublicKey[:]))
	if err != nil {
		return fmt.Errorf("failed to check that target rotation key can be used: %w", err)
	}

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- app.broker.Subscribe(ctx, rotatekeyconfig.SourceQueue(), app.handleMessage)
	}()

	slog.Info("rotatekey service started")
	startupSpan.End()

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case err := <-app.targetKeyNotUsableChan:
		slog.Error("target key no longer usable", slog.Any("error", err))
		cancel()

		return err
	case sig := <-sigc:
		slog.Info("received signal, shutting down gracefully", slog.String("signal", sig.String()))
		cancel()

		// Subscribe returns once the handler that was running has finished
		// and acked its message; broker.shutdown_grace cancels the handler's
		// context after that long, but the handler decides when it returns.
		// A second signal skips the wait.
		select {
		case err := <-consumeErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("consumer failure during shutdown", slog.Any("error", err))
			}
		case sig := <-sigc:
			slog.Warn("received a second signal, not waiting for the running handler", slog.String("signal", sig.String()))
		}

		return nil
	case err := <-consumeErr:
		if !errors.Is(err, context.Canceled) {
			slog.Error("consumer failure", slog.Any("error", err), slog.String("source-queue", rotatekeyconfig.SourceQueue()))
			cancel()

			return err
		}

		return nil
	}
}

func (app *rotateKey) handleMessage(ctx context.Context, message *broker.Message) ([]func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, span := observability.StartSpan(ctx, "handleMessage", attribute.String("message-key", message.Key))
	defer span.End()

	err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", app.schemaPath), message.Body)
	if err != nil {
		span.Error("validation of incoming message failed", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "validation of incoming message failed")}, nil
	}

	var keyRotation schema.KeyRotation
	// we unmarshal the message in the validation step so this is safe to do
	if err := json.Unmarshal(message.Body, &keyRotation); err != nil {
		span.Error("failed to unmarshal incoming message", err)

		// send message to error queue and do not requeue
		return []func(){app.errorQueue(ctx, message, "failed to unmarshal incoming message")}, nil
	}

	span.Info(
		"Received work",
		slog.String("file-id", keyRotation.FileID),
		slog.String("type", keyRotation.Type),
	)

	// Fetch rotate key hash before starting work so that we make sure the hash state
	// has not changed since the application startup.
	keyHash := hex.EncodeToString(app.targetPublicKey[:])
	// exit app if target key was modified after app start-up, e.g. if key has been deprecated
	if err := app.checkKeyHash(ctx, keyHash); err != nil {
		span.Error("failed to check that target rotation key can be used", err)

		if errors.Is(err, ErrorKeyDeprecated) || errors.Is(err, ErrorKeyNotRegistered) {
			app.targetKeyNotUsableChan <- err
		}

		return nil, err
	}

	// Get current keyhash for the file, send to error queue if this fails
	oldKeyHash, err := app.db.GetKeyHash(ctx, keyRotation.FileID)
	if err != nil {
		span.Error("failed to get file key hash", err)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			return []func(){app.errorQueue(ctx, message, "file key hash not found")}, nil
		default:
			return nil, err
		}
	}

	if oldKeyHash == keyHash {
		span.Info("file already encrypted with the target c4gh key")

		return nil, nil
	}

	oldHeader, err := app.db.GetHeader(ctx, keyRotation.FileID)
	if err != nil {
		span.Error("failed to get file header", err)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			return []func(){app.errorQueue(ctx, message, "file header not found")}, nil
		default:
			return nil, err
		}
	}

	tx, err := app.db.BeginTransaction(ctx)
	if err != nil {
		span.Error("failed to begin transaction", err)

		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			span.Error("failed to rollback transaction", err)
		}
	}()

	if err := tx.BackupHeader(ctx, keyRotation.FileID, oldHeader, oldKeyHash); err != nil {
		span.Error("failed to get back up header",
			err,
			slog.String("file-id", keyRotation.FileID),
		)
		// We Nack and requeue because if backup fails, rotation should not proceed
		return nil, err
	}

	newHeader, err := app.reencryptHeader(ctx, oldHeader)
	if err != nil {
		span.Error("failed to reencrypt old header", err)

		return nil, err
	}

	// Rotate header and keyhash in database
	if err := tx.RotateHeaderKey(ctx, newHeader, keyHash, keyRotation.FileID); err != nil {
		span.Error("failed to rotate file header key", err)

		return nil, err
	}

	// Send re-verify message
	reverificationData, err := tx.GetReVerificationDataFromFileID(ctx, keyRotation.FileID)
	if err != nil {
		span.Error("failed to get reverification data for file", err)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			return []func(){app.errorQueue(ctx, message, "file reverification data not found")}, nil
		default:
			return nil, err
		}
	}

	reVerify := schema.IngestionVerification{
		User:        reverificationData.SubmissionUser,
		FilePath:    reverificationData.SubmissionFilePath,
		FileID:      reverificationData.FileID,
		ArchivePath: reverificationData.ArchiveFilePath,
		EncryptedChecksums: []schema.Checksums{{
			Type:  reverificationData.ArchivedCheckSumType,
			Value: reverificationData.ArchivedCheckSum,
		}},
		ReVerify: true,
	}
	reVerifyMsg, _ := json.Marshal(&reVerify)

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", app.schemaPath), reVerifyMsg); err != nil {
		span.Error("validation of outgoing re-verify message failed", err)

		return []func(){app.errorQueue(ctx, message, "validation of outgoing re-verify message failed")}, nil
	}

	if err := tx.Commit(); err != nil {
		span.Error("failed to commit transaction", err)

		return nil, err
	}

	if err := app.broker.Publish(ctx, app.reverifyRoutingKey, broker.Message{
		Key:  reverificationData.FileID,
		Body: reVerifyMsg,
	}); err != nil {
		span.Error("failed to publish re verify message after database transaction committed",
			err,
			slog.String("routing-key", app.reverifyRoutingKey),
		)

		return []func(){app.errorQueue(ctx, message, "failed to publish re verify message after database transaction committed")}, nil
	}

	return nil, nil
}

var ErrorKeyDeprecated = errors.New("key deprecated")
var ErrorKeyNotRegistered = errors.New("key not registered")

// Check that a key hash exists in the database
func (app *rotateKey) checkKeyHash(ctx context.Context, keyhash string) error {
	hashes, err := app.db.ListKeyHashes(ctx)
	if err != nil {
		return err
	}

	for n := range hashes {
		if hashes[n].Hash == keyhash && hashes[n].DeprecatedAt == "" {
			return nil
		}

		if hashes[n].Hash == keyhash && hashes[n].DeprecatedAt != "" {
			return ErrorKeyDeprecated
		}
	}

	return ErrorKeyNotRegistered
}

func (app *rotateKey) errorQueue(ctx context.Context, originMessage *broker.Message, errorQueueReason string) func() {
	return func() {
		// Using context.WithoutCancel as this will run as a callback func after handleMessage ctx is canceled, but keeping context to start span under it
		ctx, span := observability.StartSpan(context.WithoutCancel(ctx), "errorQueue", attribute.String("error-queue-reason", errorQueueReason))
		defer span.End()

		if originMessage.Headers == nil {
			originMessage.Headers = make(map[string]any)
		}
		originMessage.Headers["error-queue-reason"] = errorQueueReason
		if err := app.broker.Publish(ctx, "error", *originMessage); err != nil {
			span.Error("failed to publish to error queue", err)
		}
	}
}

func (app *rotateKey) reencryptHeader(ctx context.Context, oldHeader []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, app.reencryptClientTimeout)
	defer cancel()
	reencryptHeaderResponse, err := app.reencryptClient.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{
		Publickey: app.targetPublicKeyPemEncoded,
		Oldheader: oldHeader,
	})
	if err != nil {
		return nil, err
	}

	return reencryptHeaderResponse.Header, nil
}
