package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var dbPort, mqPort, OIDCport int
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

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
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
	code := m.Run()

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
	// cleanup temp files
	_ = os.RemoveAll(ECPath)
	_ = os.RemoveAll(RSAPath)

	os.Exit(code)
}

type TestSuite struct {
	suite.Suite
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
	RBAC        []byte
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
		Exchange: "sda",
		Vhost:    "/sda",
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
func (suite *TestSuite) SetupSuite() {
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

	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	suite.RBAC = []byte(`{"policy":[{"role":"admin","path":"/c4gh-keys/*","action":"(GET)|(POST)|(PUT)"},
	{"role":"submission","path":"/dataset/create","action":"POST"},
	{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
	{"role":"submission","path":"/file/ingest","action":"POST"},
	{"role":"submission","path":"/file/accession","action":"POST"},
	{"role":"submission","path":"/users","action":"GET"},
	{"role":"submission","path":"/users/:username/files","action":"GET"},
	{"role":"*","path":"/files","action":"GET"}],
	"roles":[{"role":"admin","rolebinding":"submission"},
	{"role":"dummy","rolebinding":"admin"}]}`)
}
func (suite *TestSuite) TearDownSuite() {
	assert.NoError(suite.T(), os.RemoveAll(suite.Path))
}
func (suite *TestSuite) SetupTest() {
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

	_, err = Conf.API.DB.DB.Exec("TRUNCATE sda.files CASCADE")
	assert.NoError(suite.T(), err)

	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	// purge the queue so that the test passes when all tests are run as well as when run standalone.
	client := http.Client{Timeout: 30 * time.Second}
	for _, queue := range []string{"accession", "archived", "ingest", "mappings", "verified"} {
		req, err := http.NewRequest(http.MethodDelete, "http://"+BrokerAPI+"/api/queues/sda/"+queue+"/contents", http.NoBody)
		assert.NoError(suite.T(), err, "failed to generate query")
		req.SetBasicAuth("guest", "guest")
		res, err := client.Do(req)
		assert.NoError(suite.T(), err, "failed to query broker")
		res.Body.Close()
	}
}

func (suite *TestSuite) TestDatabasePingCheck() {
	emptyDB := database.SDAdb{}
	assert.Error(suite.T(), checkDB(&emptyDB, 1*time.Second), "nil DB should fail")

	db, err := database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), checkDB(db, 1*time.Second), "ping should succeed")
}

func (suite *TestSuite) TestAPIGetFiles() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	r.GET("/files", rbac(e), func(c *gin.Context) {
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
	err = Conf.API.DB.SetAccessionID("stableID", fileID)
	if err != nil {
		suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "stableID", fileID)
	}
	latestStatus = "ready"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, suite.User, "{}", "{}")
	assert.NoError(suite.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	assert.NoError(suite.T(), err)
	assert.NotContains(suite.T(), string(data), "accessionID")

	err = json.Unmarshal(data, &filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 1)
	assert.Equal(suite.T(), filesData[0].Status, latestStatus)
	assert.Empty(suite.T(), filesData[0].AccessionID)

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

func (suite *TestSuite) TestRBAC() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/c4gh-keys/list", nil)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/c4gh-keys/list", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "ok")
}

func (suite *TestSuite) TestRBAC_badUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.RBACpolicy = []byte(`{"policy":[{"role":"admin","path":"/admin/*","action":"(GET)|(POST)|(PUT)"}],
	"roles":[{"role":"admin","rolebinding":"submission"},
	{"role":"dummy","rolebinding":"submission"}]}`)
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&Conf.API.RBACpolicy))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/list-users", nil)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/admin/list-users", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusUnauthorized, okResponse.StatusCode)
}

func (suite *TestSuite) TestRBAC_noToken() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&[]byte{}))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	_, router := gin.CreateTestContext(w)
	router.GET("/", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "no access token supplied")
}

func (suite *TestSuite) TestRBAC_emptyPolicy() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&[]byte{}))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/files", nil)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/files", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "not authorized")
}
func (suite *TestSuite) TestIngestFile() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: user, FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", rbac(e), ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/ingest", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestIngestFile_NoUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "", FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
}
func (suite *TestSuite) TestIngestFile_WrongUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "foo", FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "sql: no rows in result set")
}

func (suite *TestSuite) TestIngestFile_WrongFilePath() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "dummy", FilePath: "bad/path"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/file/ingest", bytes.NewBuffer(ingestMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "sql: no rows in result set")
}

func (suite *TestSuite) TestSetAccession() {
	user := "dummy"
	filePath := "/inbox/dummy/file11.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: filePath, User: user})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/accession", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestSetAccession_WrongUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: "/inbox/dummy/file11.c4gh", User: "fooBar"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (suite *TestSuite) TestSetAccession_WrongFormat() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/federated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: "/inbox/dummy/file11.c4gh", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (suite *TestSuite) TestCreateDataset() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", rbac(e), createDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	assert.NoError(suite.T(), json.Unmarshal(body, &data), "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestCreateDataset_BadFormat() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}")
	assert.NoError(suite.T(), err, "got (%v) when marking file as ready", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/federated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", rbac(e), createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(suite.T(), err)
	response.Body.Close()

	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(suite.T(), string(body), "does not match pattern")
}

func (suite *TestSuite) TestCreateDataset_MissingAccessionIDs() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{}, DatasetID: "failure", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(suite.T(), err)
	response.Body.Close()

	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(suite.T(), string(body), "at least one accessionID is reqired")
}

func (suite *TestSuite) TestCreateDataset_WrongIDs() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(suite.T(), err)
	response.Body.Close()

	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(suite.T(), string(body), "accession ID not found: ")
}

func (suite *TestSuite) TestCreateDataset_WrongUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "tester"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(suite.T(), err)
	response.Body.Close()

	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(suite.T(), string(body), "accession ID owned by other user")
}

func (suite *TestSuite) TestReleaseDataset() {
	user := "TestReleaseDataset"
	for i := 0; i < 3; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
		if err != nil {
			suite.FailNow("failed to update satus of file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", user, i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_TestReleaseDataset_00", "accession_TestReleaseDataset_01", "accession_TestReleaseDataset_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/API:dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestReleaseDataset_NoDataset() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusNotFound, okResponse.StatusCode)
}

func (suite *TestSuite) TestReleaseDataset_BadDataset() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/non-existing", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	defer response.Body.Close()
	assert.Equal(suite.T(), http.StatusNotFound, response.StatusCode)
}

func (suite *TestSuite) TestReleaseDataset_DeprecatedDataset() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 5; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				suite.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	if err := Conf.API.DB.UpdateDatasetEvent("test-dataset-01", "deprecated", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/test-dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	defer response.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
}

func (suite *TestSuite) TestListActiveUsers() {
	testUsers := []string{"User-A", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 3; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), user)
			if err != nil {
				suite.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_User-A_00", "accession_User-A_01", "accession_User-A_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users", rbac(e), listActiveUsers)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	var users []string
	err = json.NewDecoder(okResponse.Body).Decode(&users)
	assert.NoError(suite.T(), err, "failed to list users from DB")
	assert.Equal(suite.T(), []string{"User-B", "User-C"}, users)
}

func (suite *TestSuite) TestListUserFiles() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 5; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				suite.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users/user@example.org/files", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users/:username/files", rbac(e), listUserFiles)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	files := []database.SubmissionFileInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&files)
	assert.NoError(suite.T(), err, "failed to list users from DB")
	assert.Equal(suite.T(), 2, len(files))
	assert.Contains(suite.T(), files[0].AccessionID, "accession_user_example.org_0")
}

func (suite *TestSuite) TestAddC4ghHash() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}

	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	// Create a valid request body
	keyhash := schema.C4ghPubKey{
		PubKey:      "LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS0KdWxGRUF6SmZZNEplUEVDZWd3YmJrVVdLNnZ2SE9SWStqMTRGdVpWVnYwND0KLS0tLS1FTkQgQ1JZUFQ0R0ggUFVCTElDIEtFWS0tLS0tCg==",
		Description: "Test key description",
	}
	body, err := json.Marshal(keyhash)
	assert.NoError(suite.T(), err)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	// Isert pubkey again and expect error
	resp2, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusConflict, resp2.StatusCode)
	defer resp2.Body.Close()
}

func (suite *TestSuite) TestAddC4ghHash_emptyJson() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	// Create an invalid request body
	body := []byte(`{"invalid_json"}`)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (suite *TestSuite) TestAddC4ghHash_notBase64() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&suite.RBAC))
	if err != nil {
		suite.T().Logf("failure: %v", err)
		suite.FailNow("failed to setup RBAC enforcer")
	}
	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	// Create an invalid request body
	body := []byte(`{"pubkey": "asdkjsahfd=", "decription": ""}`)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (suite *TestSuite) TestListC4ghHashes() {
	assert.NoError(suite.T(), Conf.API.DB.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")

	expectedResponse := database.C4ghKeyHash{
		Hash:         "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23",
		Description:  "this is a test key",
		CreatedAt:    time.Now().UTC().Format(time.DateTime),
		DeprecatedAt: "",
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	r := gin.Default()
	r.GET("/c4gh-keys/list", listC4ghHashes)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	req, err := http.NewRequest("GET", ts.URL+"/c4gh-keys/list", nil)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	hashes := []database.C4ghKeyHash{}
	err = json.NewDecoder(resp.Body).Decode(&hashes)
	assert.NoError(suite.T(), err, "failed to list users from DB")
	for n, h := range hashes {
		if h.Hash == "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23" {
			assert.Equal(suite.T(), expectedResponse, hashes[n])

			break
		}
	}
}

func (suite *TestSuite) TestDeprecateC4ghHash() {
	assert.NoError(suite.T(), Conf.API.DB.AddKeyHash("abc8f5cc8d936ce437a52cd9991453839581fc69ee26e0daefde6a5d2660fc23", "this is a deprecation test key"), "failed to register key in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	r := gin.Default()
	r.POST("/c4gh-keys/deprecate/*keyHash", deprecateC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/deprecate/abc8f5cc8d936ce437a52cd9991453839581fc69ee26e0daefde6a5d2660fc23", http.NoBody)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	// a second time gives an error since the key is alreadu deprecated
	resp2, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, resp2.StatusCode)
	defer resp2.Body.Close()
}

func (suite *TestSuite) TestDeprecateC4ghHash_wrongHash() {
	assert.NoError(suite.T(), Conf.API.DB.AddKeyHash("abc8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99", "this is a deprecation test key"), "failed to register key in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	r := gin.Default()
	r.POST("/c4gh-keys/deprecate/*keyHash", deprecateC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(suite.T(), setupJwtAuth())

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/deprecate/xyz8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", http.NoBody)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)

	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (suite *TestSuite) TestListDatasets() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/dummy/TestGetUserFiles-00%d.c4gh", i), "dummy")
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "dummy", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_dummy_00", "accession_dummy_01", "accession_dummy_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_dummy_03", "accession_dummy_04"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets/list", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets/list", listAllDatasets)
	router.GET("/dataset/list", listAllDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(suite.T(), err, "failed to list datasets from DB")
	assert.Equal(suite.T(), 2, len(datasets))
	assert.Equal(suite.T(), "released", datasets[1].Status)
	assert.Equal(suite.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (suite *TestSuite) TestListUserDatasets() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/user_example.org/TestGetUserFiles-00%d.c4gh", i), strings.ReplaceAll("user_example.org", "_", "@"))
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "user_example.org", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_user_example.org_03", "accession_user_example.org_04"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets/list/user@example.org", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets/list/:username", listUserDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(suite.T(), err, "failed to list datasets from DB")
	assert.Equal(suite.T(), 2, len(datasets))
	assert.Equal(suite.T(), "released", datasets[1].Status)
	assert.Equal(suite.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (suite *TestSuite) TestListDatasetsAsUser() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/user_example.org/TestGetUserFiles-00%d.c4gh", i), suite.User)
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_user_example.org_0%d", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_user_example.org_03", "accession_user_example.org_04"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		suite.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets", listDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(suite.T(), err, "failed to list datasets from DB")
	assert.Equal(suite.T(), 2, len(datasets))
	assert.Equal(suite.T(), "released", datasets[1].Status)
	assert.Equal(suite.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (suite *TestSuite) TestReVerifyFile() {
	user := "TestReVerify"
	for i := 0; i < 3; i++ {
		filePath := fmt.Sprintf("/%v/TestReVerify-00%d.c4gh", user, i)
		fileID, err := Conf.API.DB.RegisterFile(filePath, user)
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		if err := Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}"); err != nil {
			suite.FailNow("failed to update satus of file in database")
		}
		encSha := sha256.New()
		_, err = encSha.Write([]byte("Checksum"))
		if err != nil {
			suite.FailNow("failed to calculate Checksum")
		}

		decSha := sha256.New()
		_, err = decSha.Write([]byte("DecryptedChecksum"))
		if err != nil {
			suite.FailNow("failed to calculate DecryptedChecksum")
		}

		fileInfo := database.FileInfo{
			Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
			Size:              1000,
			Path:              filePath,
			DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
			DecryptedSize:     948,
		}
		if err := Conf.API.DB.SetArchived(fileInfo, fileID); err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		if err := Conf.API.DB.SetVerified(fileInfo, fileID, fileID); err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", user, i)
		if err := Conf.API.DB.SetAccessionID(stableID, fileID); err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
		if err := Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}"); err != nil {
			suite.FailNowf("got (%s) when updating file status: %s", err.Error(), filePath)
		}
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/file/verify/accession_TestReVerify_01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/file/verify/:accession", reVerifyFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/archived", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestReVerifyFile_wrongAccession() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/file/verify/accession_TestReVerify_99", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/verify/:accession", reVerifyFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusNotFound, okResponse.StatusCode)
}

func (suite *TestSuite) TestReVerifyDataset() {
	user := "TestReVerifyDataset"
	for i := 0; i < 3; i++ {
		filePath := fmt.Sprintf("/%v/TestReVerifyDataset-00%d.c4gh", user, i)
		fileID, err := Conf.API.DB.RegisterFile(filePath, user)
		if err != nil {
			suite.FailNow("failed to register file in database")
		}

		if err := Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}"); err != nil {
			suite.FailNow("failed to update satus of file in database")
		}
		encSha := sha256.New()
		_, err = encSha.Write([]byte("Checksum"))
		if err != nil {
			suite.FailNow("failed to calculate Checksum")
		}

		decSha := sha256.New()
		_, err = decSha.Write([]byte("DecryptedChecksum"))
		if err != nil {
			suite.FailNow("failed to calculate DecryptedChecksum")
		}

		fileInfo := database.FileInfo{
			Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
			Size:              1000,
			Path:              filePath,
			DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
			DecryptedSize:     948,
		}
		if err := Conf.API.DB.SetArchived(fileInfo, fileID); err != nil {
			suite.FailNow("failed to mark file as Archived")
		}

		if err := Conf.API.DB.SetVerified(fileInfo, fileID, fileID); err != nil {
			suite.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("%s_0%d", user, i)
		if err := Conf.API.DB.SetAccessionID(stableID, fileID); err != nil {
			suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
		if err := Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}"); err != nil {
			suite.FailNowf("got (%s) when updating file status: %s", err.Error(), filePath)
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"TestReVerifyDataset_00", "TestReVerifyDataset_01", "TestReVerifyDataset_02"}); err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dataset/verify/test-dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/dataset/verify/*dataset", reVerifyDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the messages shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/archived", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 3, data.MessagesReady)
}

func (suite *TestSuite) TestReVerifyDataset_wrongDatasetName() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dataset/verify/wrong_dataset", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/dataset/verify/*dataset", reVerifyDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusNotFound, okResponse.StatusCode)
}
