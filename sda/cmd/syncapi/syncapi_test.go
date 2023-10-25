package main

import (
	"bytes"
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
	"github.com/gorilla/mux"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	log "github.com/sirupsen/logrus"
)

var dbPort, mqPort int

type SyncAPITest struct {
	suite.Suite
}

func TestSyncAPITestSuite(t *testing.T) {
	suite.Run(t, new(SyncAPITest))
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

		query := "SELECT MAX(version) FROM sda.dbschema_version"
		var dbVersion int

		return db.QueryRow(query).Scan(&dbVersion)
	}); err != nil {
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "rabbitmq",
		Tag:        "3-management-alpine",
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
	req, err := http.NewRequest(http.MethodGet, "http://"+mqHostAndPort+"/api/users", http.NoBody)
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

func (suite *SyncAPITest) SetupTest() {
	viper.Set("log.level", "debug")
	viper.Set("log.format", "json")

	viper.Set("broker.host", "127.0.0.1")
	viper.Set("broker.port", mqPort)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "mappings")
	viper.Set("broker.exchange", "amq.direct")
	viper.Set("broker.vhost", "/")

	viper.Set("db.host", "127.0.0.1")
	viper.Set("db.port", dbPort)
	viper.Set("db.user", "postgres")
	viper.Set("db.password", "rootpasswd")
	viper.Set("db.database", "sda")
	viper.Set("db.sslmode", "disable")

	viper.Set("schema.type", "isolated")

	viper.Set("sync.api.user", "dummy")
	viper.Set("sync.api.password", "admin")
}

func (suite *SyncAPITest) TestSetup() {
	suite.SetupTest()

	conf, err := config.NewConfig("sync-api")
	assert.NoError(suite.T(), err, "Failed to setup config")
	assert.Equal(suite.T(), mqPort, conf.Broker.Port)
	assert.Equal(suite.T(), mqPort, viper.GetInt("broker.port"))

	server := setup(conf)
	assert.Equal(suite.T(), "0.0.0.0:8080", server.Addr)
}

func (suite *SyncAPITest) TestShutdown() {
	suite.SetupTest()
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "127.0.0.1", Conf.API.MQ.Conf.Host)

	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "127.0.0.1", Conf.API.DB.Config.Host)

	// make sure all conections are alive
	assert.Equal(suite.T(), false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), false, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), nil, Conf.API.DB.DB.Ping())

	shutdown()
	assert.Equal(suite.T(), true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), true, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), "sql: database is closed", Conf.API.DB.DB.Ping().Error())
}

func (suite *SyncAPITest) TestReadinessResponse() {
	suite.SetupTest()
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	r := mux.NewRouter()
	r.HandleFunc("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconneciton
	Conf.API.MQ.Connection.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the channel to force a reconneciton
	Conf.API.MQ.Channel.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close DB connection to force a reconnection
	Conf.API.DB.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()
}

func (suite *SyncAPITest) TestDatabasePingCheck() {
	suite.SetupTest()
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	noDB := database.SDAdb{}
	assert.Error(suite.T(), checkDB(&noDB, 1*time.Second), "nil DB should fail")

	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), checkDB(Conf.API.DB, 1*time.Second), "ping should succeed")
}

func (suite *SyncAPITest) TestDatasetRoute() {
	suite.SetupTest()
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	Conf.Broker.SchemasPath = "../../schemas/isolated/"

	r := mux.NewRouter()
	r.HandleFunc("/dataset", dataset)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"user": "test.user@example.com", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e6", "dataset_files": [{"filepath": "inbox/user/file-1.c4gh","file_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "sha256": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}, {"filepath": "inbox/user/file2.c4gh","file_id": "ed6af454-d910-49e3-8cda-488a6f246e76", "sha256": "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b"}]}`)
	good, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": []}`)
	bad, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()

	fileID, err := Conf.API.DB.RegisterFile("/user/file-1.c4gh", "test.user@example.com")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.SetAccessionID("5fe7b660-afea-4c3a-88a9-3daabf055ebb", fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	fileID, err = Conf.API.DB.RegisterFile("/user/file-2.c4gh", "test.user@example.com")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.SetAccessionID("ed6af454-d910-49e3-8cda-488a6f246e76", fileID)
	assert.NoError(suite.T(), err, "got (%v) when getting file archive information", err)

	accessions := []string{"5fe7b660-afea-4c3a-88a9-3daabf055ebb", "ed6af454-d910-49e3-8cda-488a6f246e76"}
	diSet := map[string][]string{
		"cd532362-e06e-4460-8490-b9ce64b8d9e6": accessions[0:1],
	}

	for di, acs := range diSet {
		err := Conf.API.DB.MapFilesToDataset(di, acs)
		assert.NoError(suite.T(), err, "failed to map file to dataset")
	}

	exists, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusAlreadyReported, exists.StatusCode)
	defer good.Body.Close()
}

func (suite *SyncAPITest) TestMetadataRoute() {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "../../schemas"

	r := mux.NewRouter()
	r.HandleFunc("/metadata", metadata)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	good, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "phail", "metadata": {}}`)
	bad, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}

func (suite *SyncAPITest) TestBuildJSON() {
	suite.SetupTest()
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	m := []byte(`{"type":"mapping", "dataset_id": "cd532362-e06e-4461-8490-b9ce64b8d9e7", "accession_ids": ["ed6af454-d910-49e3-8cda-488a6f246e67"]}`)
	_, err := buildSyncDatasetJSON(m)
	assert.EqualError(suite.T(), err, "sql: no rows in result set")

	fileID, err := Conf.API.DB.RegisterFile("dummy.user/test/file1.c4gh", "dummy.user")
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.SetAccessionID("ed6af454-d910-49e3-8cda-488a6f246e67", fileID)
	assert.NoError(suite.T(), err)

	checksum := sha256.New()
	fileInfo := database.FileInfo{Checksum: sha256.New(), Size: 1234, Path: "dummy.user/test/file1.c4gh", DecryptedChecksum: checksum, DecryptedSize: 999}
	corrID := uuid.New().String()

	err = Conf.API.DB.SetArchived(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")
	err = Conf.API.DB.MarkCompleted(fileInfo, fileID, corrID)
	assert.NoError(suite.T(), err, "failed to mark file as Verified")

	accessions := []string{"ed6af454-d910-49e3-8cda-488a6f246e67"}
	assert.NoError(suite.T(), Conf.API.DB.MapFilesToDataset("cd532362-e06e-4461-8490-b9ce64b8d9e7", accessions), "failed to map file to dataset")

	jsonData, err := buildSyncDatasetJSON(m)
	assert.NoError(suite.T(), err)
	dataset := []byte(`{"dataset_id":"cd532362-e06e-4461-8490-b9ce64b8d9e7","dataset_files":[{"filepath":"dummy.user/test/file1.c4gh","file_id":"ed6af454-d910-49e3-8cda-488a6f246e67","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}],"user":"dummy.user"}`)
	assert.Equal(suite.T(), dataset, jsonData)
}

func (suite *SyncAPITest) TestSendPOST() {
	r := http.NewServeMux()
	r.HandleFunc("/dataset", func(w http.ResponseWriter, r *http.Request) {
		_, err = w.Write([]byte(fmt.Sprint(http.StatusOK)))
		assert.NoError(suite.T(), err)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.SyncAPI = config.SyncAPIConf{
		RemoteHost:     ts.URL,
		RemoteUser:     "test",
		RemotePassword: "test",
	}
	syncJSON := []byte(`{"user":"test.user@example.com", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": [{"filepath": "inbox/user/file1.c4gh","file_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "sha256": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}, {"filepath": "inbox/user/file2.c4gh","file_id": "ed6af454-d910-49e3-8cda-488a6f246e76", "sha256": "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b"}]}`)
	err := sendPOST(syncJSON)
	assert.NoError(suite.T(), err)
}

func (suite *SyncAPITest) TestCreateHostURL() {
	Conf = &config.Config{}
	Conf.SyncAPI = config.SyncAPIConf{
		RemoteHost: "http://localhost",
		RemotePort: 443,
	}

	s, err := createHostURL(Conf.SyncAPI.RemoteHost, Conf.SyncAPI.RemotePort)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "http://localhost:443/dataset", s)
}

func (suite *SyncAPITest) TestBasicAuth() {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "../../schemas"
	Conf.SyncAPI = config.SyncAPIConf{
		APIUser:     "dummy",
		APIPassword: "test",
	}

	r := mux.NewRouter()
	r.HandleFunc("/metadata", basicAuth(metadata))
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	req, err := http.NewRequest("POST", ts.URL+"/metadata", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	req.SetBasicAuth(Conf.SyncAPI.APIUser, Conf.SyncAPI.APIPassword)
	good, err := ts.Client().Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	req.SetBasicAuth(Conf.SyncAPI.APIUser, "wrongpass")
	bad, err := ts.Client().Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusUnauthorized, bad.StatusCode)
	defer bad.Body.Close()
}
