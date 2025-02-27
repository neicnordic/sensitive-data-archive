package main

import (
	"crypto/rand"
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

func (suite *TestSuite) SetupSuite() {
	viper.Set("log.level", "debug")
	tempDir := suite.T().TempDir()
	keyFile1 := fmt.Sprintf("%s/c4gh1.key", tempDir)
	keyFile2 := fmt.Sprintf("%s/c4gh2.key", tempDir)

	publicKey, err := helper.CreatePrivateKeyFile(keyFile1, "test")
	if err != nil {
		suite.FailNow("Failed to create c4gh key")
	}
	// Add only the first public key to the list
	suite.pubKeyList = append(suite.pubKeyList, publicKey)

	_, err = helper.CreatePrivateKeyFile(keyFile2, "test")
	if err != nil {
		suite.FailNow("Failed to create c4gh key")
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

	suite.ingest.Conf, err = config.NewConfig("ingest")
	if err != nil {
		suite.FailNowf("failed to init config: %s", err.Error())
	}
	suite.ingest.DB, err = database.NewSDAdb(suite.ingest.Conf.Database)
	if err != nil {
		suite.FailNowf("failed to setup database connection: %s", err.Error())
	}
	suite.ingest.MQ, err = broker.NewMQ(suite.ingest.Conf.Broker)
	if err != nil {
		suite.FailNowf("failed to setup rabbitMQ connection: %s", err.Error())
	}
	suite.ingest.ArchiveKeyList, err = config.GetC4GHprivateKeys()
	if err != nil {
		suite.FailNow("no private keys configured")
	}

	if err := suite.ingest.DB.AddKeyHash(hex.EncodeToString(publicKey[:]), "the test key"); err != nil {
		suite.FailNow("failed to register the public key")
	}
}

func (suite *TestSuite) TearDownSuite() {
	_ = os.RemoveAll(suite.ingest.Conf.Archive.Posix.Location)
	_ = os.RemoveAll(suite.ingest.Conf.Inbox.Posix.Location)
}

func (suite *TestSuite) SetupTest() {
	var err error
	suite.ingest.Conf.Archive.Posix.Location, err = os.MkdirTemp("", "archive")
	if err != nil {
		suite.FailNow("failed to create temp folder")
	}

	suite.ingest.Conf.Inbox.Posix.Location, err = os.MkdirTemp("", "inbox")
	if err != nil {
		suite.FailNow("failed to create temp folder")
	}

	f, err := os.CreateTemp(suite.ingest.Conf.Inbox.Posix.Location, "")
	if err != nil {
		suite.FailNow("failed to create test file")
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		suite.FailNow("failed to write data to test file")
	}

	outFileName := f.Name() + ".c4gh"
	outFile, err := os.Create(outFileName)
	if err != nil {
		suite.FailNow("failed to create encrypted test file")
	}
	defer outFile.Close()

	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		suite.FailNow("failed to create private c4gh key")
	}

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(outFile, privateKey, suite.pubKeyList, nil)
	if err != nil {
		suite.FailNow("failed to create c4gh writer")
	}

	_, err = io.Copy(crypt4GHWriter, io.LimitReader(rand.Reader, 10*1024*1024))
	if err != nil {
		suite.FailNow("failed to write data to encrypted test file")
	}
	crypt4GHWriter.Close()

	suite.filePath = filepath.Base(outFileName)

	suite.ingest.Archive, err = storage.NewBackend(suite.ingest.Conf.Archive)
	if err != nil {
		suite.FailNow("failed to setup archive backend")
	}
	suite.ingest.Inbox, err = storage.NewBackend(suite.ingest.Conf.Inbox)
	if err != nil {
		suite.FailNow("failed to setup inbox backend")
	}
}
func (suite *TestSuite) TestTryDecrypt_wrongFile() {
	tempDir := suite.T().TempDir()
	err := os.WriteFile(fmt.Sprintf("%s/dummy.file", tempDir), []byte("hello\ngo\n"), 0600)
	assert.NoError(suite.T(), err)

	file, err := os.Open(fmt.Sprintf("%s/dummy.file", tempDir))
	assert.NoError(suite.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(suite.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), privateKeys, 2)

	header, err := tryDecrypt(privateKeys[0], buf)
	assert.Nil(suite.T(), header)
	assert.EqualError(suite.T(), err, "not a Crypt4GH file")
}
func (suite *TestSuite) TestTryDecrypt() {
	_, signingKey, err := keys.GenerateKeyPair()
	assert.NoError(suite.T(), err)

	// encrypt test file
	tempDir := suite.T().TempDir()
	unencryptedFile, err := os.CreateTemp(tempDir, "unencryptedFile-")
	assert.NoError(suite.T(), err)

	err = os.WriteFile(unencryptedFile.Name(), []byte("content"), 0600)
	assert.NoError(suite.T(), err)

	encryptedFile, err := os.CreateTemp(tempDir, "encryptedFile-")
	assert.NoError(suite.T(), err)

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(encryptedFile, signingKey, suite.pubKeyList, nil)
	assert.NoError(suite.T(), err)

	_, err = io.Copy(crypt4GHWriter, unencryptedFile)
	assert.NoError(suite.T(), err)
	crypt4GHWriter.Close()

	file, err := os.Open(encryptedFile.Name())
	assert.NoError(suite.T(), err)
	defer file.Close()
	buf, err := io.ReadAll(file)
	assert.NoError(suite.T(), err)

	privateKeys, err := config.GetC4GHprivateKeys()
	assert.NoError(suite.T(), err)

	for i, key := range privateKeys {
		header, err := tryDecrypt(key, buf)
		switch {
		case i == 0:
			assert.NoError(suite.T(), err)
			assert.NotNil(suite.T(), header)
		default:
			assert.Contains(suite.T(), err.Error(), "could not find matching public key heade")
			assert.Nil(suite.T(), header)
		}
	}
}

// messages of type `cancel`
func (suite *TestSuite) TestCancelFile() {
	// prepare the DB entries
	UserName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage.c4gh", UserName)
	fileID, err := suite.ingest.DB.RegisterFile(file1, UserName)
	assert.NoError(suite.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = suite.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		suite.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "cancel",
		FilePath: file1,
		User:     UserName,
	}

	assert.Equal(suite.T(), "ack", suite.ingest.cancelFile(corrID, message))
}
func (suite *TestSuite) TestCancelFile_wrongCorrelationID() {
	// prepare the DB entries
	UserName := "test-cancel"
	file1 := fmt.Sprintf("/%v/TestCancelMessage_wrongCorrelationID.c4gh", UserName)
	fileID, err := suite.ingest.DB.RegisterFile(file1, UserName)
	assert.NoError(suite.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	if err = suite.ingest.DB.UpdateFileEventLog(fileID, "uploaded", corrID, UserName, "{}", "{}"); err != nil {
		suite.Fail("failed to update file event log")
	}

	message := schema.IngestionTrigger{
		Type:     "cancel",
		FilePath: file1,
		User:     UserName,
	}

	assert.Equal(suite.T(), "reject", suite.ingest.cancelFile(uuid.New().String(), message))
}
