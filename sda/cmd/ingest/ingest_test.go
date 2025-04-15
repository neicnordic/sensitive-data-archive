package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var DBport, MQport int
var BrokerAPI string

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}
	_, b, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(b), "../../../")

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	postgres, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15.2-alpine3.17",
		Env: []string{
			"POSTGRES_PASSWORD=rootpasswd",
			"POSTGRES_DB=sda",
		},
		Mounts: []string{
			fmt.Sprintf("%s/postgresql/initdb.d:/docker-entrypoint-initdb.d", rootDir),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	dbHostAndPort := postgres.GetHostPort("5432/tcp")
	DBport, _ = strconv.Atoi(postgres.GetPort("5432/tcp"))
	databaseURL := fmt.Sprintf("postgres://postgres:rootpasswd@%s/sda?sslmode=disable", dbHostAndPort)

	pool.MaxWait = 120 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			log.Println(err)

			return err
		}

		query := "SELECT MAX(version) FROM sda.dbschema_version;"
		var dbVersion int

		return db.QueryRow(query).Scan(&dbVersion)
	}); err != nil {
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sensitive-data-archive",
		Tag:        "v0.3.89-rabbitmq",
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not start resource: %s", err)
	}

	MQport, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	BrokerAPI = rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/", http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth("guest", "guest")

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil || res.StatusCode != 200 {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to rabbitmq: %s", err)
	}

	log.Println("starting tests")
	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(rabbitmq); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestIngestTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

type TestSuite struct {
	suite.Suite
	filePath   string
	pubKeyList [][32]byte
	ingest     Ingest
}

func (ts *TestSuite) SetupSuite() {
	viper.Set("log.level", "debug")
	tempDir := ts.T().TempDir()
	keyFile1 := fmt.Sprintf("%s/c4gh1.key", tempDir)
	keyFile2 := fmt.Sprintf("%s/c4gh2.key", tempDir)

	publicKey, err := helper.CreatePrivateKeyFile(keyFile1, "test")
	if err != nil {
		ts.FailNow("Failed to create c4gh key")
	}
	// Add only the first public key to the list
	ts.pubKeyList = append(ts.pubKeyList, publicKey)

	_, err = helper.CreatePrivateKeyFile(keyFile2, "test")
	if err != nil {
		ts.FailNow("Failed to create c4gh key")
	}

	viper.Set("c4gh.privateKeys", []config.C4GHprivateKeyConf{
		{FilePath: keyFile1, Passphrase: "test"},
		{FilePath: keyFile2, Passphrase: "test"},
	})
	viper.Set("archive.type", "posix")
	viper.Set("archive.location", "/tmp/")
	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", MQport)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "ingest")
	viper.Set("broker.routingkey", "verify")
	viper.Set("broker.vhost", "sda")
	viper.Set("db.host", "localhost")
	viper.Set("db.port", DBport)
	viper.Set("db.user", "postgres")
	viper.Set("db.password", "rootpasswd")
	viper.Set("db.database", "sda")
	viper.Set("db.sslMode", "disable")
	viper.Set("inbox.type", "posix")
	viper.Set("inbox.location", "/tmp/")
	viper.Set("schema.path", "../../schemas/isolated/")

	ts.ingest.Conf, err = config.NewConfig("ingest")
	if err != nil {
		ts.FailNowf("failed to init config: %s", err.Error())
	}
	ts.ingest.DB, err = database.NewSDAdb(ts.ingest.Conf.Database)
	if err != nil {
		ts.FailNowf("failed to setup database connection: %s", err.Error())
	}
	ts.ingest.MQ, err = broker.NewMQ(ts.ingest.Conf.Broker)
	if err != nil {
		ts.FailNowf("failed to setup rabbitMQ connection: %s", err.Error())
	}
	ts.ingest.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil {
		ts.FailNow("no private keys configured")
	}

	if err := ts.ingest.DB.AddKeyHash(hex.EncodeToString(publicKey[:]), "the test key"); err != nil {
		ts.FailNow("failed to register the public key")
	}
}

func (ts *TestSuite) TearDownSuite() {
	_ = os.RemoveAll(ts.ingest.Conf.Archive.Posix.Location)
	_ = os.RemoveAll(ts.ingest.Conf.Inbox.Posix.Location)
}

func (ts *TestSuite) SetupTest() {
	var err error
	ts.ingest.Conf.Archive.Posix.Location, err = os.MkdirTemp("", "archive")
	if err != nil {
		ts.FailNow("failed to create temp folder")
	}

	ts.ingest.Conf.Inbox.Posix.Location, err = os.MkdirTemp("", "inbox")
	if err != nil {
		ts.FailNow("failed to create temp folder")
	}

	f, err := os.CreateTemp(ts.ingest.Conf.Inbox.Posix.Location, "")
	if err != nil {
		ts.FailNow("failed to create test file")
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to test file")
	}

	outFileName := f.Name() + ".c4gh"
	outFile, err := os.Create(outFileName)
	if err != nil {
		ts.FailNow("failed to create encrypted test file")
	}
	defer outFile.Close()

	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("failed to create private c4gh key")
	}

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(outFile, privateKey, ts.pubKeyList, nil)
	if err != nil {
		ts.FailNow("failed to create c4gh writer")
	}

	_, err = io.Copy(crypt4GHWriter, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to encrypted test file")
	}
	crypt4GHWriter.Close()

	ts.filePath = filepath.Base(outFileName)

	ts.ingest.Archive, err = storage.NewBackend(ts.ingest.Conf.Archive)
	if err != nil {
		ts.FailNow("failed to setup archive backend")
	}
	ts.ingest.Inbox, err = storage.NewBackend(ts.ingest.Conf.Inbox)
	if err != nil {
		ts.FailNow("failed to setup inbox backend")
	}
}
func (ts *TestSuite) TestTryDecrypt_wrongFile() {
	tempDir := ts.T().TempDir()
	err := os.WriteFile(fmt.Sprintf("%s/dummy.file", tempDir), []byte("hello\ngo\n"), 0600)
	assert.NoError(ts.T(), err)

	file, err := os.Open(fmt.Sprintf("%s/dummy.file", tempDir))
	assert.NoError(ts.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(ts.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)
	assert.Len(ts.T(), privateKeys, 2)

	header, err := tryDecrypt(privateKeys[0], buf)
	assert.Nil(ts.T(), header)
	assert.EqualError(ts.T(), err, "not a Crypt4GH file")
}
func (ts *TestSuite) TestTryDecrypt() {
	_, signingKey, err := keys.GenerateKeyPair()
	assert.NoError(ts.T(), err)

	// encrypt test file
	tempDir := ts.T().TempDir()
	unencryptedFile, err := os.CreateTemp(tempDir, "unencryptedFile-")
	assert.NoError(ts.T(), err)

	err = os.WriteFile(unencryptedFile.Name(), []byte("content"), 0600)
	assert.NoError(ts.T(), err)

	encryptedFile, err := os.CreateTemp(tempDir, "encryptedFile-")
	assert.NoError(ts.T(), err)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(encryptedFile, signingKey, ts.pubKeyList, nil)
	assert.NoError(ts.T(), err)

	_, err = io.Copy(crypt4GHWriter, unencryptedFile)
	assert.NoError(ts.T(), err)
	crypt4GHWriter.Close()

	file, err := os.Open(encryptedFile.Name())
	assert.NoError(ts.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(ts.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)

	for i, key := range privateKeys {
		header, err := tryDecrypt(key, buf)
		switch {
		case i == 0:
			assert.NoError(ts.T(), err)
			assert.NotNil(ts.T(), header)
		default:
			assert.Contains(ts.T(), err.Error(), "could not find matching public key heade")
			assert.Nil(ts.T(), header)
		}
	}
}

// messages of type `cancel`
func (ts *TestSuite) TestCancelFile() {
	// prepare the DB entries
	UserName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage.c4gh", UserName)
	fileID, err := ts.ingest.DB.RegisterFile(file1, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "cancel",
		FilePath: file1,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.cancelFile(corrID, message))
}
func (ts *TestSuite) TestCancelFile_wrongCorrelationID() {
	// prepare the DB entries
	UserName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage_wrongCorrelationID.c4gh", UserName)
	fileID, err := ts.ingest.DB.RegisterFile(file1, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "cancel",
		FilePath: file1,
		User:     UserName,
	}

	assert.Equal(ts.T(), "reject", ts.ingest.cancelFile(uuid.New().String(), message))
}

// messages of type `ingest`
func (ts *TestSuite) TestIngestFile() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))
}
func (ts *TestSuite) TestIngestFile_secondTime() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	// file is already in `archived` state
	assert.Equal(ts.T(), "reject", ts.ingest.ingestFile(corrID, message))
}
func (ts *TestSuite) TestIngestFile_unknownInboxType() {
	UserName := "test-ingest-unknown"
	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(uuid.New().String(), message))
}
func (ts *TestSuite) TestIngestFile_reingestCancelledFile() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "disabled", corrID, "ingest", "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))
}
func (ts *TestSuite) TestIngestFile_reingestCancelledFileNewChecksum() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "disabled", corrID, "ingest", "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	// over write the encrypted file to generate new checksum
	f, err := os.CreateTemp(ts.ingest.Conf.Inbox.Posix.Location, "")
	if err != nil {
		ts.FailNow("failed to create test file")
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to test file")
	}

	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("failed to create private c4gh key")
	}

	outFile, err := os.Create(path.Join(ts.ingest.Conf.Inbox.Posix.Location, ts.filePath))
	if err != nil {
		ts.FailNowf("failed to create encrypted test file: %s", err.Error())
	}
	defer outFile.Close()

	sha256hash := sha256.New()
	mr := io.MultiWriter(outFile, sha256hash)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(mr, privateKey, ts.pubKeyList, nil)
	if err != nil {
		ts.FailNowf("failed to create c4gh writer: %s", err.Error())
	}

	_, err = io.Copy(crypt4GHWriter, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to encrypted test file")
	}
	crypt4GHWriter.Close()

	// reingestion should work
	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	// DB should have the new checksum
	var dbChecksum string
	const q = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	if err := ts.ingest.DB.DB.QueryRow(q, fileID).Scan(&dbChecksum); err != nil {
		ts.FailNow("failed to get checksum from database")
	}

	assert.Equal(ts.T(), dbChecksum, hex.EncodeToString(sha256hash.Sum(nil)))
}
func (ts *TestSuite) TestIngestFile_reingestVerifiedFile() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	// fake file verification
	sha256hash := sha256.New()
	var fi database.FileInfo
	fi.ArchiveChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	if err := ts.ingest.DB.SetVerified(fi, fileID); err != nil {
		ts.Fail("failed to mark file as verified")
	}

	assert.Equal(ts.T(), "reject", ts.ingest.ingestFile(corrID, message))
}
func (ts *TestSuite) TestIngestFile_reingestVerifiedCancelledFile() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	// fake file verification
	sha256hash := sha256.New()
	var fi database.FileInfo
	fi.ArchiveChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	if err := ts.ingest.DB.SetVerified(fi, fileID); err != nil {
		ts.Fail("failed to mark file as verified")
	}

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "disabled", corrID, "ingest", "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))
}
func (ts *TestSuite) TestIngestFile_reingestVerifiedCancelledFileNewChecksum() {
	// prepare the DB entries
	UserName := "test-ingest"
	fileID, err := ts.ingest.DB.RegisterFile(ts.filePath, UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: ts.filePath,
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	var firstDbChecksum string
	const q1 = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	if err := ts.ingest.DB.DB.QueryRow(q1, fileID).Scan(&firstDbChecksum); err != nil {
		ts.FailNow("failed to get checksum from database")
	}

	// fake file verification
	verifiedSha256 := sha256.New()
	var fi database.FileInfo
	fi.ArchiveChecksum = hex.EncodeToString(verifiedSha256.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(verifiedSha256.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	if err := ts.ingest.DB.SetVerified(fi, fileID); err != nil {
		ts.Fail("failed to mark file as verified")
	}

	if err = ts.ingest.DB.UpdateFileEventLog(fileID, "disabled", corrID, "ingest", "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	// over write the encrypted file to generate new checksum
	f, err := os.CreateTemp(ts.ingest.Conf.Inbox.Posix.Location, "")
	if err != nil {
		ts.FailNow("failed to create test file")
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to test file")
	}

	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("failed to create private c4gh key")
	}

	outFile, err := os.Create(path.Join(ts.ingest.Conf.Inbox.Posix.Location, ts.filePath))
	if err != nil {
		ts.FailNowf("failed to create encrypted test file: %s", err.Error())
	}
	defer outFile.Close()

	sha256hash := sha256.New()
	mr := io.MultiWriter(outFile, sha256hash)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(mr, privateKey, ts.pubKeyList, nil)
	if err != nil {
		ts.FailNowf("failed to create c4gh writer: %s", err.Error())
	}

	_, err = io.Copy(crypt4GHWriter, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to encrypted test file")
	}
	crypt4GHWriter.Close()

	// reingestion should work
	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))

	// DB should have the new checksum
	var dbChecksum string
	const q = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	if err := ts.ingest.DB.DB.QueryRow(q, fileID).Scan(&dbChecksum); err != nil {
		ts.FailNow("failed to get checksum from database")
	}

	assert.Equal(ts.T(), dbChecksum, hex.EncodeToString(sha256hash.Sum(nil)))

	assert.NotEqual(ts.T(), dbChecksum, firstDbChecksum)
}
func (ts *TestSuite) TestIngestFile_missingFile() {
	// prepare the DB entries
	UserName := "test-ingest"
	corrID := uuid.New().String()
	basepath := filepath.Dir(ts.filePath)

	message := schema.IngestionTrigger{
		Type:     "ingest",
		FilePath: fmt.Sprintf("%s/missing.file.c4gh", basepath),
		User:     UserName,
	}

	assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(corrID, message))
}
