package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"

	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	log "github.com/sirupsen/logrus"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	ingestconf "github.com/neicnordic/sensitive-data-archive/cmd/ingest/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/config"

	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var mqPort int
var dbPort uint16
var brokerAPI string

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}
	_, b, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(b), "../../../")
	ingestconf.SetSchemaPath(path.Join(rootDir, "sda/schemas/isolated/"))

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
	postgresContainer, err := pool.RunWithOptions(&dockertest.RunOptions{
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

	dbHostAndPort := postgresContainer.GetHostPort("5432/tcp")
	dbPortUint64, _ := strconv.ParseUint(postgresContainer.GetPort("5432/tcp"), 10, 16)
	dbPort = uint16(dbPortUint64)
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
		if err := pool.Purge(postgresContainer); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not start resource: %s", err)
	}

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	brokerAPI = rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+brokerAPI+"/api/queues/sda/", http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth("guest", "guest")

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req) // #nosec G704 -- request controlled by unit test
		if err != nil || res.StatusCode != 200 {
			return err
		}
		_ = res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(postgresContainer); err != nil {
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
	if err := pool.Purge(postgresContainer); err != nil {
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
	tempDir    string
	UserName   string
	archiveDir string
	inboxDir   string

	verificationDB *sql.DB
}

func (ts *TestSuite) SetupSuite() {
	var err error
	viper.Set("log.level", "debug")
	ts.tempDir = ts.T().TempDir()
	keyFile1 := fmt.Sprintf("%s/c4gh1.key", ts.tempDir)
	keyFile2 := fmt.Sprintf("%s/c4gh2.key", ts.tempDir)

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
	viper.Set("broker.port", mqPort)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "ingest")
	viper.Set("broker.routingkey", "verify")
	viper.Set("broker.vhost", "sda")
	viper.Set("schema.path", "../../schemas/isolated/")

	ts.ingest.db, err = postgres.NewPostgresSQLDatabase(
		postgres.Host("127.0.0.1"),
		postgres.Port(dbPort),
		postgres.User("postgres"),
		postgres.Password("rootpasswd"),
		postgres.DatabaseName("sda"),
		postgres.Schema("sda"),
		postgres.CACert(""),
		postgres.SslMode("disable"),
		postgres.ClientCert(""),
		postgres.ClientKey(""),
	)
	if err != nil {
		ts.FailNow("failed to connect to database", err)
	}

	ts.verificationDB, err = sql.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%d user=postgres password=rootpasswd dbname=sda sslmode=disable search_path=sda", dbPort))
	if err != nil {
		ts.FailNow(fmt.Sprintf("failed to connect to database: %v", err))
	}

	ts.ingest.Broker = &MockBroker{}
	if err != nil {
		ts.FailNowf("failed to setup rabbitMQ connection: %s", err.Error())
	}
	ts.ingest.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil {
		ts.FailNow("no private keys configured")
	}

	if err := ts.ingest.db.AddKeyHash(context.Background(), hex.EncodeToString(publicKey[:]), "the test key"); err != nil {
		ts.FailNow("failed to register the public key")
	}

	ts.UserName = "test-ingest"
}

func (ts *TestSuite) TearDownSuite() {
	if ts.verificationDB != nil {
		ts.NoError(ts.verificationDB.Close())
	}
	if ts.ingest.db != nil {
		ts.NoError(ts.ingest.db.Close())
	}
}

func (ts *TestSuite) SetupTest() {
	ts.archiveDir = ts.T().TempDir()
	ts.inboxDir = ts.T().TempDir()

	// Ensure a folder with the user name exists
	err := os.Mkdir(path.Join(ts.inboxDir, ts.UserName), 0750)
	if err != nil {
		ts.FailNow("failed to create user folder in inbox directory")
	}

	f, err := os.CreateTemp(path.Join(ts.inboxDir, ts.UserName), "")
	if err != nil {
		ts.FailNow("failed to create test file")
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		ts.FailNow("failed to write data to test file")
	}

	outFileName := f.Name() + ".c4gh"
	outFile, err := os.Create(outFileName) // #nosec G703 -- file controlled by unit test
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

	if err := os.WriteFile(filepath.Join(ts.tempDir, "config.yaml"), []byte(fmt.Sprintf(`
storage:
  inbox:
    posix:
      - path: %s
  archive:
    posix:
      - path: %s
`, ts.inboxDir, ts.archiveDir)), 0600); err != nil {
		ts.FailNow(err.Error())
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetConfigFile(filepath.Join(ts.tempDir, "config.yaml"))
	if err := viper.ReadInConfig(); err != nil {
		ts.FailNow(err.Error())
	}

	lb, err := locationbroker.NewLocationBroker(ts.ingest.db)
	ts.NoError(err)
	ts.ingest.ArchiveWriter, err = storage.NewWriter(context.Background(), "archive", lb)
	if err != nil {
		ts.FailNow("failed to setup archive writer")
	}
	ts.ingest.ArchiveReader, err = storage.NewReader(context.Background(), "archive")
	if err != nil {
		ts.FailNow("failed to setup archive reader")
	}
	ts.ingest.InboxReader, err = storage.NewReader(context.Background(), "inbox")
	if err != nil {
		ts.FailNow("failed to setup inbox reader")
	}

	viper.Set("c4gh.privateKeys", []config.C4GHprivateKeyConf{
		{FilePath: filepath.Join(ts.tempDir, "c4gh1.key"), Passphrase: "test"},
		{FilePath: filepath.Join(ts.tempDir, "c4gh2.key"), Passphrase: "test"},
	})
}

func (ts *TestSuite) TestCancelFile_BaseCase() {
	userName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userName)
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, "/inbox", file1, userName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", userName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	assert.NoError(ts.T(), ts.ingest.db.SetArchived(context.Background(), ts.archiveDir, &database.FileInfo{
		ArchivedChecksum:  "123",
		Size:              500,
		Path:              fileID,
		DecryptedChecksum: "321",
		DecryptedSize:     550,
		UploadedChecksum:  "abc",
	}, fileID))

	ts.NoError(os.WriteFile(filepath.Join(ts.archiveDir, fileID), []byte("unit testing file"), 0600))

	message := createMessage("cancel", file1, userName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), err, nil)
}

func (ts *TestSuite) TestCancelFile_NotArchived() {
	userName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage.c4gh", userName)
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, "/inbox", file1, userName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", userName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("cancel", file1, userName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when canceling file")
}

func (ts *TestSuite) TestCancelFile_WrongCorrelationID() {
	userName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage_wrongCorrelationID.c4gh", userName)
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, "/inbox", file1, userName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", userName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("cancel", file1, userName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestIngestFile_BaseCase() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")
}

func (ts *TestSuite) TestIngestFile_NoSubmissionLocation() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, "/inbox", ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestIngestFile_AlreadyIngested() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	if err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.Error(ts.T(), err)
}

func (ts *TestSuite) TestIngestFile_UnknownInboxType() {
	message := createMessage("ingest", ts.filePath, ts.UserName, uuid.New().String())
	_, err := ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), nil, err)
}

func (ts *TestSuite) TestIngestFile_IngestDisabledFile() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "disabled", "ingest", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")
}

func (ts *TestSuite) TestIngestFile_IngestDisabledFileNewChecksum() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")
	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	// first ingestion with original inbox reader
	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "disabled", "ingest", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	// generate new encrypted file in memory
	_, privateKey, err := keys.GenerateKeyPair()
	assert.NoError(ts.T(), err, "failed to generate c4gh key pair")

	var buf bytes.Buffer
	sha256hash := sha256.New()
	mr := io.MultiWriter(&buf, sha256hash)
	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(mr, privateKey, ts.pubKeyList, nil)
	assert.NoError(ts.T(), err, "failed to create c4gh writer")
	_, err = io.Copy(crypt4GHWriter, io.LimitReader(rand.Reader, 10*1024*1024))
	assert.NoError(ts.T(), err, "failed to write data to encrypted test file")
	crypt4GHWriter.Close()

	// swap inbox reader to serve the new file from memory
	originalReader := ts.ingest.InboxReader
	ts.ingest.InboxReader = &MockReader{data: buf.Bytes()}
	defer func() { ts.ingest.InboxReader = originalReader }()

	// reingestion should work
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	// DB should have the new checksum
	var dbChecksum string
	const q = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	err = ts.verificationDB.QueryRow(q, fileID).Scan(&dbChecksum)
	assert.NoError(ts.T(), err, "failed to get checksum from database")
	assert.Equal(ts.T(), hex.EncodeToString(sha256hash.Sum(nil)), dbChecksum)
}

func (ts *TestSuite) TestIngestFile_IngestVerifiedFile() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	sha256hash := sha256.New()
	fi := new(database.FileInfo)
	fi.ArchivedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	if err := ts.ingest.db.SetVerified(context.Background(), fi, fileID); err != nil {
		ts.Fail("failed to mark file as verified")
	}

	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.Error(ts.T(), err)
}

func (ts *TestSuite) TestIngestFile_IngestVerifiedDisabledFile() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	sha256hash := sha256.New()
	fi := new(database.FileInfo)
	fi.ArchivedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(sha256hash.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	err = ts.ingest.db.SetVerified(context.Background(), fi, fileID)
	assert.NoError(ts.T(), err, "failed to mark file as verified")

	if err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "disabled", "ingest", "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")
}

func (ts *TestSuite) TestIngestFile_IngestVerifiedCancelledFileNewChecksum() {
	fileID, err := ts.ingest.db.RegisterFile(context.Background(), nil, ts.inboxDir, ts.filePath, ts.UserName)
	assert.NoError(ts.T(), err, "failed to register file in database")

	if err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "uploaded", ts.UserName, "{}", "{}"); err != nil {
		ts.Fail("failed to update file event log")
	}

	message := createMessage("ingest", ts.filePath, ts.UserName, fileID)
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	var firstDbChecksum string
	const q1 = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	if err := ts.verificationDB.QueryRow(q1, fileID).Scan(&firstDbChecksum); err != nil {
		ts.FailNow("failed to get checksum from database")
	}

	verifiedSha256 := sha256.New()
	fi := new(database.FileInfo)
	fi.ArchivedChecksum = hex.EncodeToString(verifiedSha256.Sum(nil))
	fi.DecryptedChecksum = hex.EncodeToString(verifiedSha256.Sum(nil))
	fi.DecryptedSize = 10 * 1024 * 1024
	fi.Size = (10 * 1024 * 1024) + 456
	err = ts.ingest.db.SetVerified(context.Background(), fi, fileID)
	assert.NoError(ts.T(), err, "failed to mark file as verified")

	err = ts.ingest.db.UpdateFileEventLog(context.Background(), fileID, "disabled", "ingest", "{}", "{}")
	assert.NoError(ts.T(), err, "failed to update file event log")

	// overwrite the encrypted file to generate new checksum
	f, err := os.CreateTemp(ts.inboxDir, "")
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

	outFile, err := os.Create(path.Join(ts.inboxDir, ts.UserName, ts.filePath))
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

	// assert.Equal(ts.T(), "ack", ts.ingest.ingestFile(context.Background(), fileID, message))
	_, err = ts.ingest.handleMessage(context.Background(), message)
	assert.NoError(ts.T(), err, "unexpected error when ingesting file")

	// DB should have the new checksum
	var dbChecksum string
	const q = "SELECT checksum from sda.checksums WHERE source = 'UPLOADED' and file_id = $1;"
	if err := ts.verificationDB.QueryRow(q, fileID).Scan(&dbChecksum); err != nil {
		ts.FailNow("failed to get checksum from database")
	}

	assert.Equal(ts.T(), dbChecksum, hex.EncodeToString(sha256hash.Sum(nil)))

	assert.NotEqual(ts.T(), dbChecksum, firstDbChecksum)
}

func (ts *TestSuite) TestIngestFile_MissingFile() {
	basepath := filepath.Dir(ts.filePath)
	fileID := uuid.NewString()
	message := createMessage("ingest", fmt.Sprintf("%s/missing.file.c4gh", basepath), ts.UserName, fileID)
	_, err := ts.ingest.handleMessage(context.Background(), message)
	assert.Equal(ts.T(), err, nil)
}

func (ts *TestSuite) TestRegisterC4ghKey_NewDeployment() {
	_, err := ts.verificationDB.Exec("TRUNCATE sda.encryption_keys CASCADE;")
	assert.NoError(ts.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), 2, len(privateKeys))

	assert.NoError(ts.T(), ts.ingest.registerC4GHKey(context.Background()))

	kh, err := ts.ingest.db.ListKeyHashes(context.Background())
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), 2, len(kh))
}

func (ts *TestSuite) TestRegisterC4ghKey_ExistingEntry() {
	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), 2, len(privateKeys))

	assert.NoError(ts.T(), ts.ingest.registerC4GHKey(context.Background()))

	kh, err := ts.ingest.db.ListKeyHashes(context.Background())
	assert.NoError(ts.T(), err)
	assert.Equal(ts.T(), 1, len(kh))
}

func createMessage(triggerType, filePath, userID, messageKey string) *broker.Message {
	body := schema.IngestionTrigger{
		Type:     triggerType,
		FilePath: filePath,
		User:     userID,
	}
	bodyJSON, _ := json.Marshal(body)

	return &broker.Message{Key: messageKey, Body: bodyJSON}
}
