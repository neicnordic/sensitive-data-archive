package main

import (
	"encoding/json"
	"fmt"

	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"

	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
)

var dbPort, mqPort, OIDCport int

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
	// TODO use sda-db or postgres repo?
	postgres, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sda-db",
		Tag:        "v2.1.3",
		Env: []string{
			"DB_LEGA_IN_PASSWORD=lega_in",
			"DB_LEGA_OUT_PASSWORD=lega_out",
			"NOTLS=true",
			"POSTGRES_PASSWORD=rootpassword",
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
	// TODO this url or postgres://postgres:rootpasswd@%s/sda?sslmode=disable"
	databaseURL := fmt.Sprintf("postgres://lega_in:lega_in@%s/lega?sslmode=disable", dbHostAndPort)

	pool.MaxWait = 120 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			log.Println(err)

			return err
		}

		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	// TODO use sda-mq or rabbitmq?
	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sda-mq",
		Tag:        "v1.4.30",
		Env: []string{
			"MQ_USER=test",
			"MQ_PASSWORD_HASH=C5ufXbYlww6ZBcEqDUB04YdUptO81s+ozI3Ll5GCHTnv8NAm",
			"MQ_VHOST=test",
			"NOTLS=true",
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

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	mqHostAndPort := rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+mqHostAndPort+"/api/users", http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth("test", "test")

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

	RSAPath, _ := os.MkdirTemp("", "RSA")
	if err := helper.CreateRSAkeys(RSAPath, RSAPath); err != nil {
		log.Panic("Failed to create RSA keys")
	}
	ECPath, _ := os.MkdirTemp("", "EC")
	if err := helper.CreateECkeys(ECPath, ECPath); err != nil {
		log.Panic("Failed to create EC keys")
	}

	// OIDC container
	oidc, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "python",
		Tag:        "3.10-slim",
		Cmd: []string{
			"/bin/sh",
			"-c",
			"pip install --upgrade pip && pip install aiohttp Authlib joserfc requests && python -u /oidc.py",
		},
		ExposedPorts: []string{"8080"},
		Mounts: []string{
			fmt.Sprintf("%s/.github/integration/sda/oidc.py:/oidc.py", rootDir),
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

	OIDCport, _ = strconv.Atoi(oidc.GetPort("8080/tcp"))
	OIDCHostAndPort := oidc.GetHostPort("8080/tcp")

	client = http.Client{Timeout: 5 * time.Second}
	req, err = http.NewRequest(http.MethodGet, "http://"+OIDCHostAndPort+"/jwk", http.NoBody)
	if err != nil {
		log.Panic(err)
	}

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(oidc); err != nil {
			log.Panicf("Could not purge oidc resource: %s", err)
		}
		log.Panicf("Could not connect to oidc: %s", err)
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
	if err := pool.Purge(oidc); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
}

func TestShutdown(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       mqPort,
		User:       "test",
		Password:   "test",
		RoutingKey: "test",
		Exchange:   "sda",
		Ssl:        false,
		Vhost:      "/test",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(t, err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "lega_in",
		Password: "lega_in",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
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
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       mqPort,
		User:       "test",
		Password:   "test",
		RoutingKey: "test",
		Exchange:   "sda",
		Ssl:        false,
		Vhost:      "/test",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(t, err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "lega_in",
		Password: "lega_in",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(t, err)

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconnection
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

type TestSuite struct {
	suite.Suite
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
	Token       string
	User        string
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupTest() {

	suite.Path = "/tmp/keys/"
	suite.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	privpath, pubpath, err := helper.MakeFolder(suite.Path)
	assert.NoError(suite.T(), err)
	suite.PrivatePath = privpath
	suite.PublicPath = pubpath
	err = helper.CreateRSAkeys(privpath, pubpath)
	assert.NoError(suite.T(), err)

	// Create a valid token for queries to the API
	prKeyParsed, err := helper.ParsePrivateRSAKey(suite.PrivatePath, "/rsa")
	assert.NoError(suite.T(), err)
	token, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)
	suite.Token = token
	user, ok := helper.DefaultTokenClaims["sub"].(string)
	assert.True(suite.T(), ok)
	suite.User = user

	c := &config.Config{}
	ServerConf := config.ServerConfig{}
	ServerConf.Jwtpubkeypath = suite.PublicPath
	c.Server = ServerConf

	Conf = c

	log.Print("Setup DB for my test")
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "lega_in",
		Password: "lega_in",
		Database: "lega",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

}

func TestDatabasePingCheck(t *testing.T) {
	database := database.SDAdb{}
	assert.Error(t, checkDB(&database, 1*time.Second), "nil DB should fail")

	database.DB, _, err = sqlmock.New()
	assert.NoError(t, err)
	assert.NoError(t, checkDB(&database, 1*time.Second), "ping should succeed")
}

func (suite *TestSuite) TestAPIAuthenticate() {

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/files", func(c *gin.Context) {
		getFiles(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	filesURL := ts.URL + "/files"
	client := &http.Client{}

	setupJwtAuth()

	// No credentials
	resp, err := http.Get(filesURL)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusUnauthorized, resp.StatusCode)
	defer resp.Body.Close()

	// Valid credentials

	req, err := http.NewRequest("GET", filesURL, nil)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)
	resp, err = client.Do(req)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	assert.NoError(suite.T(), err)
	defer resp.Body.Close()
}

func (suite *TestSuite) TestAPIGetFiles() {

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/files", func(c *gin.Context) {
		getFiles(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	filesURL := ts.URL + "/files"
	client := &http.Client{}

	setupJwtAuth()

	req, err := http.NewRequest("GET", filesURL, nil)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)

	// Test query when no files is in db
	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	filesData := []database.SubmissionFileInfo{}
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 0)
	log.Printf("it is %v", filesData)
	assert.NoError(suite.T(), err)

	// Insert a file and make sure it is listed
	fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestAPIGetFiles.c4gh", suite.User), suite.User)
	assert.NoError(suite.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	latestStatus := "uploaded"
	err = Conf.API.DB.UpdateFileStatus(fileID, latestStatus, corrID, suite.User, "{}")
	assert.NoError(suite.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 1)
	assert.Equal(suite.T(), filesData[0].Status, latestStatus)
	assert.NoError(suite.T(), err)

	// Update the file's status and make sure only the lastest status is listed
	latestStatus = "ready"
	err = Conf.API.DB.UpdateFileStatus(fileID, latestStatus, corrID, suite.User, "{}")
	assert.NoError(suite.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 1)
	assert.Equal(suite.T(), filesData[0].Status, latestStatus)

	assert.NoError(suite.T(), err)
}
