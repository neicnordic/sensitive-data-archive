package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var dbPort int

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

	log.Println("starting tests")
	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	pvo := docker.PruneVolumesOptions{Filters: make(map[string][]string), Context: context.Background()}
	if _, err := pool.Client.PruneVolumes(pvo); err != nil {
		log.Fatalf("could not prune docker volumes: %s", err.Error())
	}

	os.Exit(code)
}

func (suite *SyncTest) SetupTest() {
	viper.Set("log.level", "debug")
	viper.Set("archive.type", "posix")
	viper.Set("archive.location", "../../dev_utils")
	viper.Set("sync.destination.type", "posix")
	viper.Set("sync.destination.location", "../../dev_utils")

	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "test")
	viper.Set("db.host", "localhost")
	viper.Set("db.port", dbPort)
	viper.Set("db.user", "postgres")
	viper.Set("db.password", "rootpasswd")
	viper.Set("db.database", "sda")
	viper.Set("db.sslmode", "disable")
	viper.Set("sync.centerPrefix", "prefix")
	viper.Set("sync.remote.host", "http://remote.example")
	viper.Set("sync.remote.user", "user")
	viper.Set("sync.remote.password", "pass")

	key := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	keyPath, _ := os.MkdirTemp("", "key")
	err := os.WriteFile(keyPath+"/c4gh.key", []byte(key), 0600)
	assert.NoError(suite.T(), err)

	viper.Set("c4gh.filepath", keyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")

	pubKey := "-----BEGIN CRYPT4GH PUBLIC KEY-----\nuQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=\n-----END CRYPT4GH PUBLIC KEY-----"
	err = os.WriteFile(keyPath+"/c4gh.pub", []byte(pubKey), 0600)
	assert.NoError(suite.T(), err)
	viper.Set("c4gh.syncPubKeyPath", keyPath+"/c4gh.pub")

	defer os.RemoveAll(keyPath)
}

func (suite *SyncTest) TestBuildSyncDatasetJSON() {
	suite.SetupTest()
	Conf, err := config.NewConfig("sync")
	assert.NoError(suite.T(), err)

	db, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	fileID, err := db.RegisterFile("dummy.user/test/file1.c4gh", "dummy.user")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = db.SetAccessionID("ed6af454-d910-49e3-8cda-488a6f246e67", fileID)
	assert.NoError(suite.T(), err)

	checksum := fmt.Sprintf("%x", sha256.New().Sum(nil))
	fileInfo := database.FileInfo{Checksum: fmt.Sprintf("%x", sha256.New().Sum(nil)), Size: 1234, Path: "dummy.user/test/file1.c4gh", DecryptedChecksum: checksum, DecryptedSize: 999}
	corrID := uuid.New().String()

	err = db.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")
	err = db.SetVerified(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Verified")

	accessions := []string{"ed6af454-d910-49e3-8cda-488a6f246e67"}
	assert.NoError(suite.T(), db.MapFilesToDataset("cd532362-e06e-4461-8490-b9ce64b8d9e7", accessions), "failed to map file to dataset")

	m := []byte(`{"type":"mapping", "dataset_id": "cd532362-e06e-4461-8490-b9ce64b8d9e7", "accession_ids": ["ed6af454-d910-49e3-8cda-488a6f246e67"]}`)
	jsonData, err := buildSyncDatasetJSON(m)
	assert.NoError(suite.T(), err)
	dataset := []byte(`{"dataset_id":"cd532362-e06e-4461-8490-b9ce64b8d9e7","dataset_files":[{"filepath":"dummy.user/test/file1.c4gh","file_id":"ed6af454-d910-49e3-8cda-488a6f246e67","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}],"user":"dummy.user"}`)
	assert.Equal(suite.T(), string(dataset), string(jsonData))
}

func (suite *SyncTest) TestCreateHostURL() {
	conf = &config.Config{}
	conf.Sync = config.Sync{
		RemoteHost: "http://localhost",
		RemotePort: 443,
	}

	s, err := createHostURL(conf.Sync.RemoteHost, conf.Sync.RemotePort)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "http://localhost:443/dataset", s)
}

func (suite *SyncTest) TestSendPOST() {
	r := http.NewServeMux()
	r.HandleFunc("/dataset", func(w http.ResponseWriter, r *http.Request) {
		username, _, ok := r.BasicAuth()
		if ok && username == "foo" {
			w.WriteHeader(http.StatusUnauthorized)
		}

		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	conf = &config.Config{}
	conf.Sync = config.Sync{
		RemoteHost:     ts.URL,
		RemoteUser:     "test",
		RemotePassword: "test",
	}
	syncJSON := []byte(`{"user":"test.user@example.com", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": [{"filepath": "inbox/user/file1.c4gh","file_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "sha256": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}, {"filepath": "inbox/user/file2.c4gh","file_id": "ed6af454-d910-49e3-8cda-488a6f246e76", "sha256": "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b"}]}`)
	err := sendPOST(syncJSON)
	assert.NoError(suite.T(), err)

	conf.Sync = config.Sync{
		RemoteHost:     ts.URL,
		RemoteUser:     "foo",
		RemotePassword: "bar",
	}
	assert.EqualError(suite.T(), sendPOST(syncJSON), "401 Unauthorized")
}
