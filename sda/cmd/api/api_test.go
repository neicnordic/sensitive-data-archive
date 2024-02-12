package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
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

		return db.Ping()
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
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
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

type TestSuite struct {
	suite.Suite
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
	Token       string
	User        string
}

func (suite *TestSuite) TestShutdown() {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "amq.default",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	// make sure all conections are alive
	assert.Equal(suite.T(), false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), false, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), nil, Conf.API.DB.DB.Ping())

	shutdown()
	assert.Equal(suite.T(), true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), true, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), "sql: database is closed", Conf.API.DB.DB.Ping().Error())
}

func (suite *TestSuite) TestReadinessResponse() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "amq.default",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconnection
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

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupTest() {
	log.SetLevel(log.DebugLevel)
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
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

}

func (suite *TestSuite) TestDatabasePingCheck() {
	emptyDB := database.SDAdb{}
	assert.Error(suite.T(), checkDB(&emptyDB, 1*time.Second), "nil DB should fail")

	db, err := database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), checkDB(db, 1*time.Second), "ping should succeed")
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

	assert.NoError(suite.T(), setupJwtAuth())

	requestURL, err := url.Parse(filesURL)
	assert.NoError(suite.T(), err)

	// No credentials
	resp, err := http.Get(requestURL.String())
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

	assert.NoError(suite.T(), setupJwtAuth())

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
	assert.NoError(suite.T(), err)

	// Insert a file and make sure it is listed
	file1 := fmt.Sprintf("/%v/TestAPIGetFiles.c4gh", suite.User)
	fileID, err := Conf.API.DB.RegisterFile(file1, suite.User)
	assert.NoError(suite.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	latestStatus := "uploaded"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, suite.User, "{}", "{}")
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
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, suite.User, "{}", "{}")
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

	// Insert a second file and make sure it is listed
	file2 := fmt.Sprintf("/%v/TestAPIGetFiles2.c4gh", suite.User)
	_, err = Conf.API.DB.RegisterFile(file2, suite.User)
	assert.NoError(suite.T(), err, "failed to register file in database")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 2)
	for _, fileInfo := range filesData {
		switch fileInfo.InboxPath {
		case file1:
			assert.Equal(suite.T(), fileInfo.Status, latestStatus)
		case file2:
			assert.Equal(suite.T(), fileInfo.Status, "registered")
		}
	}
	assert.NoError(suite.T(), err)
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func testEndpoint(c *gin.Context) {
	c.JSON(200, gin.H{"ok": true})
}

func (suite *TestSuite) TestIsAdmin_NoToken() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	// no token should not be allowed
	router.ServeHTTP(w, r)
	badResponse := w.Result()
	defer badResponse.Body.Close()
	b, _ := io.ReadAll(badResponse.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, badResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "no access token supplied")
}
func (suite *TestSuite) TestIsAdmin_BadUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"foo", "bar"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	// non admin user should not be allowed
	r.Header.Add("Authorization", "Bearer "+suite.Token)
	router.ServeHTTP(w, r)
	notAdmin := w.Result()
	defer notAdmin.Body.Close()
	b, _ := io.ReadAll(notAdmin.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, notAdmin.StatusCode)
	assert.Contains(suite.T(), string(b), "not authorized")
}
func (suite *TestSuite) TestIsAdmin() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"foo", "bar", "dummy"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	// admin user should be allowed
	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "ok")
}
