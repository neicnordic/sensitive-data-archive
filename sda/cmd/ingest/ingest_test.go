package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"fmt"
	"io"
	"log/slog"
	"os"
	"path"

	"runtime"
	"testing"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	ingestconf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	v2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2" //nolint: revive
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

var dbPort int
var schemaPath string
var dockerTestDB *database.SDAdb
var ingest *Ingest

const dockerContainerPort = "5432/tcp"
const userID = "testuser"

func createMessage(triggerType, filePath, userID, messageKey string) *v2.Message {
	body := schema.IngestionTrigger{
		Type:     triggerType,
		FilePath: filePath,
		User:     userID,
	}
	bodyJSON, _ := json.Marshal(body)

	return &v2.Message{Key: messageKey, Body: bodyJSON}
}

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()

		return
	}

	configv2.Load()

	// tests are executed in their respective package directory but needs access to things relative to the root of the project
	_, relativePath, _, _ := runtime.Caller(0)
	projectRoot := path.Join(path.Dir(relativePath), "../../../")
	schemaPath = filepath.Join(projectRoot, "sda/", ingestconf.SchemaPath())
	localConfig := filepath.Join(projectRoot, "sda/config_local.yaml")

	viper.SetConfigFile(localConfig)
	if err := viper.ReadInConfig(); err != nil {
		slog.Error("could not read config", "err", err)
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		slog.Error("could not construct pool", "err", err)

		return
	}
	err = pool.Client.Ping()
	if err != nil {
		slog.Error("could not connect to docker", "err", err)

		return
	}

	postgres, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15.2-alpine3.17",
		Env: []string{
			fmt.Sprintf("POSTGRES_PASSWORD=%s", viper.GetString("db.password")),
			fmt.Sprintf("POSTGRES_DB=%s", viper.GetString("db.database")),
		},
		Mounts: []string{
			fmt.Sprintf("%s/postgresql/initdb.d:/docker-entrypoint-initdb.d", projectRoot),
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		slog.Error("could not start resource", "err", err)

		return
	}

	dbPort, _ = strconv.Atoi(postgres.GetPort(dockerContainerPort))
	viper.Set("db.port", dbPort)

	conf, err := config.NewConfig("ingest")
	if err != nil {
		slog.Error("could not get ingest configuration", "err", err)

		return
	}

	pool.MaxWait = 120 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", conf.Database.User, conf.Database.Password, postgres.GetHostPort(dockerContainerPort), conf.Database.Database))
		if err != nil {
			return err
		}

		return db.Ping()
	}); err != nil {
		slog.Error("could not connect to postgres", "err", err)

		return
	}

	dockerTestDB, err = database.NewSDAdb(conf.Database)
	if err != nil {
		slog.Error("could not create new database connection", "err", err)

		return
	}

	inboxPath, err := os.MkdirTemp("", "tmp-*")
	if err != nil {
		slog.Error("could not create inboxPath", "err", err)
	}

	archivePath, err := os.MkdirTemp("", "tmp-*")
	if err != nil {
		slog.Error("could not create archivePath", "err", err)
	}

	defer os.RemoveAll(inboxPath)
	defer os.RemoveAll(archivePath)

	publicKey, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		slog.Error("could not generate c4gh key pair", "err", err)

		return
	}

	archiveWriter := &MockWriter{
		WriteFileFunc: func(ctx context.Context, filePath string, fileContent io.Reader) (string, error) {
			return archivePath, nil
		},
	}

	archiveReader := &MockReader{
		FindFileFunc: func(ctx context.Context, filePath string) (string, error) {
			return "", storageerrors.ErrorFileNotFoundInLocation
		},
		GetFileSizeFunc: func(ctx context.Context, location, filePath string) (int64, error) {
			return 1024, nil
		},
		NewReaderFunc: func(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
			return makeEncryptedStream(nil, publicKey, []byte("test payload")), nil
		},
	}

	inboxReader := &MockReader{
		FindFileFunc: func(ctx context.Context, filePath string) (string, error) {
			return "", storageerrors.ErrorFileNotFoundInLocation
		},
		GetFileSizeFunc: func(ctx context.Context, location, filePath string) (int64, error) {
			return 1024, nil
		},
		NewReaderFunc: func(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
			return makeEncryptedStream(nil, publicKey, []byte("test payload")), nil
		},
	}

	mockBroker := MockBroker{}

	ingest = &Ingest{
		MQ:             &mockBroker,
		ArchiveWriter:  archiveWriter,
		ArchiveReader:  archiveReader,
		InboxReader:    inboxReader,
		DB:             dockerTestDB,
		SchemaPath:     schemaPath,
		ArchiveKeyList: []*[32]byte{&privateKey},
	}

	if err := ingest.DB.AddKeyHash(hex.EncodeToString(publicKey[:]), "the test key"); err != nil {
		slog.Error("failed to register public key", "err", err)

		return
	}

	m.Run()

	if err := pool.Purge(postgres); err != nil {
		slog.Error("could not purge postgres", "err", err)

		return
	}
}

func generateC4GHKeyPair(t testing.TB) ([32]byte, [32]byte) {
	if t != nil {
		t.Helper()
	}
	pub, priv, err := keys.GenerateKeyPair()
	if err != nil && t == nil {
		return [32]byte{}, [32]byte{}
	}
	require.NoError(t, err)

	return pub, priv
}

func makeEncryptedStream(t testing.TB, recipientPublicKey [32]byte, plaintext []byte) io.ReadCloser {
	if t != nil {
		t.Helper()
	}
	_, writerPriv := generateC4GHKeyPair(t)
	var buf bytes.Buffer
	w, err := streaming.NewCrypt4GHWriter(&buf, writerPriv, [][32]byte{recipientPublicKey}, nil)
	if err != nil && t == nil {
		t.Fatalf("failed to create crypt4gh writer: %v", err)

		return nil
	}

	if _, err := w.Write(plaintext); err != nil {
		if t == nil {
			slog.Error("failed to write plaintext", "err", err)

			return nil
		}
	}

	if err := w.Close(); err != nil {
		if t == nil {
			t.Fatalf("failed to close crypt4gh writer: %v", err)

			return nil
		}
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes()))
}

func TestIngestDecrypt_MultipleKeys_ValidKeyIsSecond(t *testing.T) {
	_, wrongPriv := generateC4GHKeyPair(t)
	validPub, validPriv := generateC4GHKeyPair(t)

	stream := makeEncryptedStream(t, validPub, []byte("secret payload"))

	app := &Ingest{
		// wrongPriv is first — loop must fall through to validPriv
		ArchiveKeyList: []*[32]byte{&wrongPriv, &validPriv},
	}

	result, err := app.decrypt(stream)
	require.NoError(t, err)

	derivedPub := keys.DerivePublicKey(validPriv)
	expectedKeyHash := hex.EncodeToString(derivedPub[:])
	assert.Equal(t, expectedKeyHash, result.keyHash, "should report the key that actually decrypted the file")
	assert.NotEmpty(t, result.header)
	assert.NotEmpty(t, result.checksum)
}

func TestIngestDecrypt_HappyPath(t *testing.T) {
	pub, priv := generateC4GHKeyPair(t)
	_, writerPriv := generateC4GHKeyPair(t)
	var rawBuf bytes.Buffer
	var streamBuf bytes.Buffer

	w, err := streaming.NewCrypt4GHWriter(io.MultiWriter(&rawBuf, &streamBuf), writerPriv, [][32]byte{pub}, nil)
	if err != nil {
		t.Fatalf("failed to create crypt4gh writer: %v", err)
	}

	plaintext := []byte("hello crypt4gh")
	if _, err := w.Write(plaintext); err != nil {
		t.Fatalf("failed to write plaintext: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close crypt4gh writer: %v", err)
	}

	stream := io.NopCloser(&streamBuf)

	app := &Ingest{
		ArchiveKeyList: []*[32]byte{&priv},
	}

	decryptionResult, err := app.decrypt(stream)
	keyHash := decryptionResult.keyHash
	header := decryptionResult.header
	checksum := decryptionResult.checksum

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	derivedPub := keys.DerivePublicKey(priv)
	expectedKeyHash := hex.EncodeToString(derivedPub[:])
	if keyHash != expectedKeyHash {
		t.Errorf("keyHash mismatch\n  got:  %s\n  want: %s", keyHash, expectedKeyHash)
	}

	h := sha256.Sum256(header)
	expectedChecksum := fmt.Sprintf("%x", h[:])
	if checksum != expectedChecksum {
		t.Errorf("checksum mismatch\n  got:  %s\n  want: %s", checksum, expectedChecksum)
	}

	if len(header) == 0 {
		t.Error("expected non-empty header bytes, got empty slice")
	}
}

func TestIngestDecrypt_NoValidKey(t *testing.T) {
	pub, _ := generateC4GHKeyPair(t)
	_, wrongPriv := generateC4GHKeyPair(t)

	stream := makeEncryptedStream(t, pub, []byte("secret"))

	app := &Ingest{
		ArchiveKeyList: []*[32]byte{&wrongPriv},
	}

	_, err := app.decrypt(stream)
	if err == nil {
		t.Fatal("expected an error for wrong key, got nil")
	}

	want := "no valid keys found to decrypt file"
	if err.Error() != want {
		t.Errorf("error message mismatch\n  got:  %q\n  want: %q", err.Error(), want)
	}
}

func TestIngestDecrypt_InvalidStream(t *testing.T) {
	_, priv := generateC4GHKeyPair(t)

	garbage := io.NopCloser(bytes.NewReader([]byte("this is not a crypt4gh file")))

	app := &Ingest{
		ArchiveKeyList: []*[32]byte{&priv},
	}

	_, err := app.decrypt(garbage)
	if err == nil {
		t.Fatal("expected an error for invalid stream, got nil")
	}
}

func TestIngestCancelFile(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	fileInfo := database.FileInfo{
		ArchiveChecksum:   "123",
		Size:              500,
		Path:              fileID,
		DecryptedChecksum: "321",
		DecryptedSize:     550,
		UploadedChecksum:  "abc",
	}

	err = ingest.DB.SetArchived("archive", fileInfo, fileID)
	assert.NoError(t, err)

	message := createMessage("cancel", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestCancelFile_NotYetArchived(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("cancel", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestCancelFile_IncorrectCorrelationID(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestCancelMessage_wrongCorrelationID.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("cancel", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestFile_ArchiveWriteFails(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestArchiveWriteFails.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	require.NoError(t, err)
	require.NoError(t, ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}"))

	// Replace archive writer so WriteFile fails
	originalWriter := ingest.ArchiveWriter
	ingest.ArchiveWriter = &MockWriter{
		WriteFileFunc: func(ctx context.Context, filePath string, content io.Reader) (string, error) {
			return "", errors.New("simulated archive write failure")
		},
		RemoveFileFunc: func(ctx context.Context, location, filePath string) error {
			return nil
		},
	}
	defer func() { ingest.ArchiveWriter = originalWriter }()

	message := createMessage("ingest", filePath, userID, fileID)
	callbacks, err := ingest.handleMessage(context.TODO(), message)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated archive write failure")
	// Error callbacks (error queue + event log) should be returned
	assert.Len(t, callbacks, 2, "should return error queue and error event callbacks")
}

func TestIngestFile_GetFileSizeFails(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestGetFileSizeFails.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	require.NoError(t, err)
	require.NoError(t, ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}"))

	originalReader := ingest.ArchiveReader
	ingest.ArchiveReader = &MockReader{
		FindFileFunc: func(ctx context.Context, filePath string) (string, error) {
			return "", storageerrors.ErrorFileNotFoundInLocation
		},
		GetFileSizeFunc: func(ctx context.Context, location, filePath string) (int64, error) {
			return 0, errors.New("simulated GetFileSize failure")
		},
		NewReaderFunc: func(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
			pubKey := keys.DerivePublicKey(*ingest.ArchiveKeyList[0])

			return makeEncryptedStream(t, pubKey, []byte("test payload")), nil
		},
	}
	defer func() { ingest.ArchiveReader = originalReader }()

	message := createMessage("ingest", filePath, userID, fileID)
	callbacks, err := ingest.handleMessage(context.TODO(), message)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated GetFileSize failure")
	assert.Len(t, callbacks, 2, "should return error queue and error event callbacks")
}

func TestIngestFile(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFile.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

// TODO: This one gives false positive, need to remove ingestlocation properly
func TestIngestFile_NoSubmissionLocation(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFileNoLocation.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestFile_AlreadyIngested(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFileDuplicate.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.Error(t, err)
}

func TestIngestFile_IngestCancelledFile(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFileCancelled.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "disabled", "ingest", "{}", "{}")
	assert.NoError(t, err)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestFile_UnknownType(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFileCancelled.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("unknown", filePath, userID, fileID)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.Error(t, err)
}

func TestIngestFile_ReingestCancelledFileNewChecksum(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestIngestFileChecksum.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	require.NoError(t, err)
	require.NoError(t, ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}"))

	message := createMessage("ingest", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	require.NoError(t, err)

	require.NoError(t, ingest.DB.UpdateFileEventLog(fileID, "disabled", "ingest", "{}", "{}"))

	// swap inbox reader to return a stream with different content, producing a new checksum
	newPayload := []byte("different payload produces different checksum")
	pub, _ := ingest.ArchiveKeyList[0], ingest.ArchiveKeyList[0]
	pubKey := keys.DerivePublicKey(*ingest.ArchiveKeyList[0])
	ingest.InboxReader.(*MockReader).NewReaderFunc = func(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
		return makeEncryptedStream(t, pubKey, newPayload), nil
	}
	defer func() {
		// restore original so other tests are unaffected
		ingest.InboxReader.(*MockReader).NewReaderFunc = func(ctx context.Context, location, filePath string) (io.ReadCloser, error) {
			return makeEncryptedStream(t, pubKey, []byte("test payload")), nil
		}
	}()
	_ = pub // suppress unused

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)

	var dbChecksum string
	const q = "SELECT checksum FROM sda.checksums WHERE source = 'UPLOADED' AND file_id = $1;"
	require.NoError(t, ingest.DB.DB.QueryRow(q, fileID).Scan(&dbChecksum))
}

func TestIngestFile_IngestVerifiedFile(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestFileVerified.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)
	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)

	// mark file as verified
	sha256hash := sha256.New()
	var fi database.FileInfo
	fi.ArchiveChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	err = ingest.DB.SetVerified(fi, fileID)
	assert.NoError(t, err)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.Error(t, err)
}

func TestIngestFile_ReingestVerifiedCancelledFile(t *testing.T) {
	filePath := fmt.Sprintf("/%v/TestFileReingestCancelled.c4gh", userID)
	fileID, err := ingest.DB.RegisterFile(nil, "/inbox", filePath, userID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "uploaded", userID, "{}", "{}")
	assert.NoError(t, err)

	message := createMessage("ingest", filePath, userID, fileID)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)

	// mark file as verified
	sha256hash := sha256.New()
	var fi database.FileInfo
	fi.ArchiveChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	err = ingest.DB.SetVerified(fi, fileID)
	assert.NoError(t, err)

	err = ingest.DB.UpdateFileEventLog(fileID, "disabled", userID, "{}", "{}")
	assert.NoError(t, err)

	_, err = ingest.handleMessage(context.TODO(), message)
	assert.NoError(t, err)
}

func TestIngestFile_MissingFile(t *testing.T) {
	message := createMessage("ingest", "somepath", userID, uuid.NewString())
	_, err := ingest.handleMessage(context.TODO(), message)
	slog.Info("err", "err", err)
	assert.Error(t, err)
}
