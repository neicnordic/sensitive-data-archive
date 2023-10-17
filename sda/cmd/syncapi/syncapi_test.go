package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func TestSetup(t *testing.T) {
	viper.Set("log.level", "debug")
	viper.Set("log.format", "json")

	viper.Set("broker.host", "test")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "test")
	viper.Set("broker.password", "test")
	viper.Set("broker.queue", "test")
	viper.Set("broker.routingkey", "test")

	viper.Set("db.host", "test")
	viper.Set("db.port", 123)
	viper.Set("db.user", "test")
	viper.Set("db.password", "test")
	viper.Set("db.database", "test")

	viper.Set("schema.type", "isolated")

	conf := config.Config{}
	conf.API.Host = "localhost"
	conf.API.Port = 8080
	server := setup(&conf)

	assert.Equal(t, "localhost:8080", server.Addr)
}

func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")
}

func TestShutdown(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       5672,
		User:       "test",
		Password:   "test",
		RoutingKey: "test",
		Exchange:   "sda",
		Ssl:        false,
		Vhost:      "/test",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}
	assert.NoError(t, err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     5432,
		User:     "lega_in",
		Password: "lega_in",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewDB(Conf.Database)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}
	assert.NoError(t, err)

	// make sure all conections are alive
	assert.Equal(t, false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(t, false, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(t, nil, Conf.API.DB.DB.Ping())

	shutdown()
	assert.Equal(t, true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(t, true, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(t, "sql: database is closed", Conf.API.DB.DB.Ping().Error())
}

func TestReadinessResponse(t *testing.T) {
	r := mux.NewRouter()
	r.HandleFunc("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       5672,
		User:       "test",
		Password:   "test",
		RoutingKey: "test",
		Exchange:   "sda",
		Ssl:        false,
		Vhost:      "/test",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}
	assert.NoError(t, err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     5432,
		User:     "lega_in",
		Password: "lega_in",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewDB(Conf.Database)
	assert.NoError(t, err)

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconneciton
	Conf.API.MQ.Connection.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the channel to force a reconneciton
	Conf.API.MQ.Channel.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close DB connection to force a reconnection
	Conf.API.DB.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()
}

func TestDatabasePingCheck(t *testing.T) {
	database := database.SQLdb{}
	assert.Error(t, checkDB(&database, 1*time.Second), "nil DB should fail")

	database.DB, _, err = sqlmock.New()
	assert.NoError(t, err)
	assert.NoError(t, checkDB(&database, 1*time.Second), "ping should succeed")
}

func TestDatasetRoute(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:        "localhost",
		Port:        5672,
		User:        "test",
		Password:    "test",
		RoutingKey:  "test",
		Exchange:    "sda",
		Ssl:         false,
		Vhost:       "/test",
		SchemasPath: "file://../../schemas/isolated/",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewDB(Conf.Database)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}

	r := mux.NewRouter()
	r.HandleFunc("/dataset", dataset)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"user":"test.user@example.com", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e6", "dataset_files": [{"filepath": "inbox/user/file1.c4gh","file_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "sha256": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}, {"filepath": "inbox/user/file2.c4gh","file_id": "ed6af454-d910-49e3-8cda-488a6f246e76", "sha256": "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b"}]}`)
	good, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": []}`)
	bad, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}

func TestMetadataRoute(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "file://../../schemas/"

	r := mux.NewRouter()
	r.HandleFunc("/metadata", metadata)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	good, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "phail", "metadata": {}}`)
	bad, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}

func TestBuildJSON(t *testing.T) {
	Conf = &config.Config{}
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewDB(Conf.Database)
	if err != nil {
		t.Skip("skip TestShutdown since broker not present")
	}

	db := Conf.API.DB.DB

	var fileID int64
	const insert = "INSERT INTO local_ega.main(submission_file_path, submission_user, decrypted_file_checksum, status, submission_file_extension) VALUES($1, $2, $3, 'READY', 'c4gh') RETURNING id;"
	const accession = "UPDATE local_ega.files SET stable_id = $1 WHERE inbox_path = $2;"
	const mapping = "INSERT INTO local_ega_ebi.filedataset(file_id, dataset_stable_id) VALUES ($1, 'cd532362-e06e-4460-8490-b9ce64b8d9e7');"

	err := db.QueryRow(insert, "dummy.user/test/file1.c4gh", "dummy.user", "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b").Scan(&fileID)
	assert.NoError(t, err)
	err = db.QueryRow(accession, "ed6af454-d910-49e3-8cda-488a6f246e76", "dummy.user/test/file1.c4gh").Err()
	assert.NoError(t, err)
	err = db.QueryRow(mapping, fileID).Err()
	assert.NoError(t, err)

	err = db.QueryRow(insert, "dummy.user/test/file2.c4gh", "dummy.user", "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6").Scan(&fileID)
	assert.NoError(t, err)
	err = db.QueryRow(accession, "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "dummy.user/test/file2.c4gh").Err()
	assert.NoError(t, err)
	err = db.QueryRow(mapping, fileID).Err()
	assert.NoError(t, err)

	m := []byte(`{"type":"mapping", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "accession_ids": ["5fe7b660-afea-4c3a-88a9-3daabf055ebb", "ed6af454-d910-49e3-8cda-488a6f246e76"]}`)
	_, err = buildSyncDatasetJSON(m)
	assert.NoError(t, err)
}

func TestSendPOST(t *testing.T) {
	r := http.NewServeMux()
	r.HandleFunc("/dataset", func(w http.ResponseWriter, r *http.Request) {
		_, err = w.Write([]byte(fmt.Sprint(http.StatusOK)))
		assert.NoError(t, err)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.Sync = config.SyncConf{
		Host:     ts.URL,
		User:     "test",
		Password: "test",
	}
	syncJSON := []byte(`{"user":"test.user@example.com", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": [{"filepath": "inbox/user/file1.c4gh","file_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "sha256": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}, {"filepath": "inbox/user/file2.c4gh","file_id": "ed6af454-d910-49e3-8cda-488a6f246e76", "sha256": "c967d96e56dec0f0cfee8f661846238b7f15771796ee1c345cae73cd812acc2b"}]}`)
	err := sendPOST(syncJSON)
	assert.NoError(t, err)
}

func TestCreateHostURL(t *testing.T) {
	Conf = &config.Config{}
	Conf.Sync = config.SyncConf{
		Host: "http://localhost",
		Port: 443,
	}

	s, err := createHostURL(Conf.Sync.Host, Conf.Sync.Port)
	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:443/dataset", s)
}

func TestBasicAuth(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "file://../../schemas/"
	Conf.API = config.APIConf{
		User:     "dummy",
		Password: "test",
	}

	r := mux.NewRouter()
	r.HandleFunc("/metadata", basicAuth(metadata))
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	req, err := http.NewRequest("POST", ts.URL+"/metadata", bytes.NewBuffer(goodJSON))
	assert.NoError(t, err)
	req.SetBasicAuth(Conf.API.User, Conf.API.Password)
	good, err := ts.Client().Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	req.SetBasicAuth(Conf.API.User, "wrongpass")
	bad, err := ts.Client().Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, bad.StatusCode)
	defer bad.Body.Close()
}
