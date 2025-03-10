package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var dbPort, mqPort int

type SyncTest struct {
	suite.Suite
}

func TestSyncTestSuite(t *testing.T) {
	suite.Run(t, new(SyncTest))
}

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
	dbPort, _ = strconv.Atoi(postgres.GetPort("5432/tcp"))
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
		Repository: "rabbitmq",
		Tag:        "3.12.14-management-alpine",
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

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	mqHostAndPort := rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPut, "http://"+mqHostAndPort+"/api/queues/%2F/mappings", http.NoBody)
	if err != nil {
		log.Fatal(err)
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
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to rabbitmq: %s", err)
	}

	log.Println("starting tests")
	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(rabbitmq); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	pvo := docker.PruneVolumesOptions{Filters: make(map[string][]string), Context: context.Background()}
	if _, err := pool.Client.PruneVolumes(pvo); err != nil {
		log.Fatalf("could not prune docker volumes: %s", err.Error())
	}
}

func (suite *SyncTest) SetupSuite() {
	viper.Set("log.level", "debug")

	viper.Set("broker.exchange", "")
	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", mqPort)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "mappings")
	viper.Set("broker.prefetchCount", 10)

	viper.Set("db.host", "localhost")
	viper.Set("db.port", dbPort)
	viper.Set("db.user", "postgres")
	viper.Set("db.password", "rootpasswd")
	viper.Set("db.database", "sda")
	viper.Set("db.sslmode", "disable")

	viper.Set("schema.type", "bp")
	viper.Set("sync.centerPrefix", "aa")

	dbConf := database.DBConf{
		Database: "sda",
		Host:     "localhost",
		Password: "rootpasswd",
		Port:     dbPort,
		SslMode:  "disable",
		User:     "postgres",
	}

	db, err := database.NewSDAdb(dbConf)
	assert.NoError(suite.T(), err)

	mqConf := broker.MQConf{
		Exchange:      "",
		Host:          "localhost",
		Password:      "guest",
		Port:          mqPort,
		User:          "guest",
		Vhost:         "/",
		PrefetchCount: 10,
	}
	mq, err := broker.NewMQ(mqConf)
	assert.NoError(suite.T(), err)

	accessions := []string{}
	for i := 2; i < 5; i++ {
		fileID, err := db.RegisterFile(fmt.Sprintf("/testuser/TestMapFilesToDataset-%d.c4gh", i), "testuser")
		assert.NoError(suite.T(), err, "failed to register file in database")

		checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
		decSize := int64(1000 + i)
		encSize := int64(1027 + i)
		fileInfo := database.FileInfo{
			Checksum:          fmt.Sprintf("%x", sha256.New().Sum(nil)),
			Size:              encSize,
			Path:              fmt.Sprintf("/testuser/TestMapFilesToDataset-%d.c4gh", i),
			DecryptedChecksum: checksum,
			DecryptedSize:     decSize,
		}
		corrID := uuid.New().String()

		err = db.SetArchived(fileInfo, fileID, corrID)
		assert.NoError(suite.T(), err, "failed to mark file as Archived")

		err = db.SetVerified(fileInfo, fileID, corrID)
		assert.NoError(suite.T(), err, "failed to mark file as Verified")

		err = db.SetAccessionID(fmt.Sprintf("aa-File-v5y9hk-nc2rf%d", i), fileID)
		assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)
	}

	assert.NoError(suite.T(), db.MapFilesToDataset("aa-Dataset-cd5323-kjsdh4", accessions), "failed to map file to dataset")

	msg := []byte(`{"type":"mapping", "dataset_id": "aa-Dataset-cd5323-kjsdh4", "accession_ids": ["aa-File-v5y9hk-nc2rf2","aa-File-v5y9hk-nc2rf3","aa-File-v5y9hk-nc2rf4"]}`)
	assert.NoError(suite.T(), mq.SendMessage("", "", "mappings", msg), "failed to send message")

}

func (suite *SyncTest) TestHandleDatasetMsg() {
	conf, err := config.NewConfig("sync-ctrl")
	assert.NoError(suite.T(), err)
	conf.Broker.SchemasPath = "../../schemas/bigpicture/"
	db, err := database.NewSDAdb(conf.Database)
	assert.NoError(suite.T(), err)

	mq, err := broker.NewMQ(conf.Broker)
	assert.NoError(suite.T(), err)

	assert.False(suite.T(), mq.Channel.IsClosed(), "closed channel")

	_, err = mq.Channel.QueueDeclare("sync_files", false, false, false, false, nil)
	assert.NoError(suite.T(), err)

	go handleDatasetMsg(conf, db, mq)

	count := amqp.Queue{Messages: 10}
	for count.Messages > 0 {
		time.Sleep(time.Second)
		count, err = mq.Channel.QueueDeclarePassive("mappings", false, false, false, false, nil)
		assert.NoError(suite.T(), err)
	}

	s, err := mq.Channel.QueueDeclarePassive("sync_files", false, false, false, false, nil)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), 3, s.Messages)

	msg := []byte(`{"type":"release", "dataset_id": "aa-Dataset-cd5323-kjsdh4"}`)
	assert.NoError(suite.T(), mq.SendMessage("", "", "mappings", msg), "failed to send message")
}
