package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
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
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
)

var dbPort, mqPort, OIDCport int
var BrokerAPI string

type server struct {
	reencrypt.UnimplementedReencryptServer
	c4ghPrivateKeyList []*[32]byte
}

func (s *server) ReencryptHeader(ctx context.Context, req *reencrypt.ReencryptRequest) (*reencrypt.ReencryptResponse, error) {
	// Mock response based on your needs
	mockedResponse := &reencrypt.ReencryptResponse{
		Header: []byte("predefined header response"),
	}

	return mockedResponse, nil
}

func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid address format: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in address '%s': %w", addr, err)
	}

	return host, port, nil
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

	rsaPath, _ := os.MkdirTemp("", "RSA")
	if err := helper.CreateRSAkeys(rsaPath, rsaPath); err != nil {
		log.Panic("Failed to create RSA keys")
	}
	ecPath, _ := os.MkdirTemp("", "EC")
	if err := helper.CreateECkeys(ecPath, ecPath); err != nil {
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
	oidcHostAndPort := oidc.GetHostPort("8080/tcp")

	client = http.Client{Timeout: 5 * time.Second}
	req, err = http.NewRequest(http.MethodGet, "http://"+oidcHostAndPort+"/jwk", http.NoBody)
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
	_ = os.RemoveAll(ecPath)
	_ = os.RemoveAll(rsaPath)

	os.Exit(code)
}

type UserKey struct {
	PublicKey      [32]byte
	PrivateKey     [32]byte
	PrivateKeyPath string
	PubKeyBase64   string
}

type GrpcListener struct {
	Listener   net.Listener
	gs         *grpc.Server
	serverOpts []grpc.ServerOption
}

type TestSuite struct {
	suite.Suite
	Path         string
	PublicPath   string
	PrivatePath  string
	KeyName      string
	RBAC         []byte
	Token        string
	User         string
	PubKeyList   [][32]byte
	PrivateKey   *[32]byte
	UserKey      UserKey
	FileData     []byte
	FileHeader   []byte
	GoodC4ghFile string
	BadC4ghFile  string
	GrpcListener GrpcListener
}

func (s *TestSuite) TestShutdown() {
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
	assert.NoError(s.T(), err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(s.T(), err)

	// make sure all conections are alive
	assert.Equal(s.T(), false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(s.T(), false, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(s.T(), nil, Conf.API.DB.DB.Ping())

	shutdown()
	assert.Equal(s.T(), true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(s.T(), true, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(s.T(), "sql: database is closed", Conf.API.DB.DB.Ping().Error())
}

func (s *TestSuite) TestReadinessResponse() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconnection
	Conf.API.MQ.Connection.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the channel to force a reconneciton
	Conf.API.MQ.Channel.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close DB connection to force a reconnection
	Conf.API.DB.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()
}

// Initialise configuration and create jwt keys
func (s *TestSuite) SetupSuite() {
	log.SetLevel(log.DebugLevel)
	s.Path = "/tmp/keys/"
	s.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	privpath, pubpath, err := helper.MakeFolder(s.Path)
	assert.NoError(s.T(), err)
	s.PrivatePath = privpath
	s.PublicPath = pubpath
	err = helper.CreateRSAkeys(privpath, pubpath)
	assert.NoError(s.T(), err)

	// Create a valid token for queries to the API
	prKeyParsed, err := helper.ParsePrivateRSAKey(s.PrivatePath, "/rsa")
	assert.NoError(s.T(), err)
	token, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(s.T(), err)
	s.Token = token
	user, ok := helper.DefaultTokenClaims["sub"].(string)
	assert.True(s.T(), ok)
	s.User = user

	c := &config.Config{}
	serverConf := config.ServerConfig{}
	serverConf.Jwtpubkeypath = s.PublicPath
	c.Server = serverConf

	Conf = c

	Conf.API.Host = "0.0.0.0"
	Conf.API.Port = 8080

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
	assert.NoError(s.T(), err)

	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(s.T(), err)

	s.RBAC = []byte(`{"policy":[{"role":"admin","path":"/c4gh-keys/*","action":"(GET)|(POST)|(PUT)"},
	{"role":"submission","path":"/dataset/create","action":"POST"},
	{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
	{"role":"submission","path":"/file/ingest*","action":"POST"},
	{"role":"submission","path":"/file/accession","action":"POST"},
	{"role":"submission","path":"/users","action":"GET"},
	{"role":"submission","path":"/users/:username/files","action":"GET"},
	{"role":"submission","path":"/users/:username/file/:fileid","action":"GET"},
	{"role":"*","path":"/files","action":"GET"}],
	"roles":[{"role":"admin","rolebinding":"submission"},
	{"role":"dummy","rolebinding":"admin"}]}`)

	Conf.Inbox.Posix.Location, err = os.MkdirTemp("", "inbox")
	if err != nil {
		s.FailNow("failed to create temp folder")
	}

	s.UserKey.PublicKey, s.UserKey.PrivateKey, err = keys.GenerateKeyPair()
	if err != nil {
		s.T().FailNow()
	}

	s.UserKey.PrivateKeyPath = s.Path + "/user.key"
	key, err := os.Create(s.UserKey.PrivateKeyPath)
	if err != nil {
		s.T().FailNow()
	}
	if err := keys.WriteCrypt4GHX25519PrivateKey(key, s.UserKey.PrivateKey, []byte("password")); err != nil {
		s.T().FailNow()
	}

	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, s.UserKey.PublicKey); err != nil {
		s.T().FailNow()
	}
	s.UserKey.PubKeyBase64 = base64.StdEncoding.EncodeToString(buf.Bytes())

	Conf.API.Grpc = config.Grpc{
		Host:    "localhost",
		Port:    50051,
		Timeout: 30,
	}

	s.FileHeader, _ = hex.DecodeString("637279707434676801000000010000006c000000000000007ca283608311dacfc32703a3cc9a2b445c9a417e036ba5943e233cfc65a1f81fdcc35036a584b3f95759114f584d1e81e8cf23a9b9d1e77b9e8f8a8ee8098c2a3e9270fe6872ef9d1c948caf8423efc7ce391081da0d52a49b1e6d0706f267d6140ff12b")
	s.FileData, _ = hex.DecodeString("e046718f01d52c626276ce5931e10afd99330c4679b3e2a43fdf18146e85bae8eaee83")

	err = os.MkdirAll(path.Join(Conf.Inbox.Posix.Location, s.User), 0750)
	assert.NoError(s.T(), err, "failed to create inbox directory")

	// Create test files in the inbox
	fileContent := []byte("This is the content of the test file.")
	contentReader := bytes.NewReader(fileContent)
	s.GoodC4ghFile = path.Join(Conf.Inbox.Posix.Location, s.User, "test_download.c4gh")
	s.BadC4ghFile = path.Join(Conf.Inbox.Posix.Location, s.User, "badc4ghfile.c4gh")

	outFile, err := os.Create(s.GoodC4ghFile)
	if err != nil {
		s.FailNow("failed to create encrypted test file")
	}
	defer outFile.Close()

	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		s.FailNow("failed to create private c4gh key")
	}

	crypt4GHWriter, err := streaming.NewCrypt4GHWriter(outFile, privateKey, s.PubKeyList, nil)
	if err != nil {
		s.FailNow("failed to create c4gh writer")
	}

	_, err = io.Copy(crypt4GHWriter, contentReader)
	if err != nil {
		s.FailNow("failed to write predefined data to encrypted test file")
	}
	crypt4GHWriter.Close()

	err = os.WriteFile(s.BadC4ghFile, []byte("bad c4gh content"), 0600)
	if err != nil {
		s.FailNow("failed to write content to file")
	}

	// create a gPRC listener for tests using the reencrypt service
	s.GrpcListener.Listener, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		s.T().FailNow()
	}

	go func() {
		var opts []grpc.ServerOption
		s.GrpcListener.gs = grpc.NewServer(opts...)
		reencrypt.RegisterReencryptServer(s.GrpcListener.gs, &server{c4ghPrivateKeyList: Conf.ReEncrypt.C4ghPrivateKeyList})
		if err := s.GrpcListener.gs.Serve(s.GrpcListener.Listener); err != nil {
			s.T().Fail()
		}
	}()
}
func (s *TestSuite) TearDownSuite() {
	assert.NoError(s.T(), os.RemoveAll(s.Path))
	assert.NoError(s.T(), os.RemoveAll(Conf.Inbox.Posix.Location))
	if s.GrpcListener.gs != nil {
		s.GrpcListener.gs.GracefulStop()
	}
	if s.GrpcListener.Listener != nil {
		s.GrpcListener.Listener.Close()
	}
}
func (s *TestSuite) SetupTest() {
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(s.T(), err)

	_, err = Conf.API.DB.DB.Exec("TRUNCATE sda.files CASCADE")
	assert.NoError(s.T(), err)

	Conf.Broker = broker.MQConf{
		Host:        "localhost",
		Port:        mqPort,
		User:        "guest",
		Password:    "guest",
		Exchange:    "sda",
		Vhost:       "/sda",
		SchemasPath: "../../schemas/isolated",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(s.T(), err)

	// purge the queue so that the test passes when all tests are run as well as when run standalone.
	client := http.Client{Timeout: 30 * time.Second}
	for _, queue := range []string{"accession", "archived", "ingest", "mappings", "verified"} {
		req, err := http.NewRequest(http.MethodDelete, "http://"+BrokerAPI+"/api/queues/sda/"+queue+"/contents", http.NoBody)
		assert.NoError(s.T(), err, "failed to generate query")
		req.SetBasicAuth("guest", "guest")
		res, err := client.Do(req)
		assert.NoError(s.T(), err, "failed to query broker")
		res.Body.Close()
	}
}

func (s *TestSuite) TestDatabasePingCheck() {
	emptyDB := database.SDAdb{}
	assert.Error(s.T(), checkDB(&emptyDB, 1*time.Second), "nil DB should fail")

	db, err := database.NewSDAdb(Conf.Database)
	assert.NoError(s.T(), err)
	assert.NoError(s.T(), checkDB(db, 1*time.Second), "ping should succeed")
}

func (s *TestSuite) TestAPIGetFiles() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	r.GET("/files", rbac(e), func(c *gin.Context) {
		getFiles(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	filesURL := ts.URL + "/files"
	client := &http.Client{}

	assert.NoError(s.T(), setupJwtAuth())

	req, err := http.NewRequest("GET", filesURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)

	// Test query when no files is in db
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	filesData := []database.SubmissionFileInfo{}
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), len(filesData), 0)
	assert.NoError(s.T(), err)

	// Insert a file and make sure it is listed
	file1 := fmt.Sprintf("/%v/TestAPIGetFiles.c4gh", s.User)
	fileID, err := Conf.API.DB.RegisterFile(file1, s.User)
	assert.NoError(s.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	latestStatus := "uploaded"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, s.User, "{}", "{}")
	assert.NoError(s.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), len(filesData), 1)
	assert.Equal(s.T(), filesData[0].Status, latestStatus)
	assert.NoError(s.T(), err)

	// Update the file's status and make sure only the lastest status is listed
	err = Conf.API.DB.SetAccessionID("stableID", fileID)
	if err != nil {
		s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), "stableID", fileID)
	}
	latestStatus = "ready"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, s.User, "{}", "{}")
	assert.NoError(s.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)
	assert.NotContains(s.T(), string(data), "accessionID")

	err = json.Unmarshal(data, &filesData)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), len(filesData), 1)
	assert.Equal(s.T(), filesData[0].Status, latestStatus)
	assert.Empty(s.T(), filesData[0].AccessionID)

	// Insert a second file and make sure it is listed
	file2 := fmt.Sprintf("/%v/TestAPIGetFiles2.c4gh", s.User)
	_, err = Conf.API.DB.RegisterFile(file2, s.User)
	assert.NoError(s.T(), err, "failed to register file in database")

	resp, err = client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), len(filesData), 2)
	for _, fileInfo := range filesData {
		switch fileInfo.InboxPath {
		case file1:
			assert.Equal(s.T(), fileInfo.Status, latestStatus)
		case file2:
			assert.Equal(s.T(), fileInfo.Status, "registered")
		default:
			s.Fail("unknown inbox path")
		}
	}
	assert.NoError(s.T(), err)
}

func (s *TestSuite) TestAPIGetFiles_filteredSelection() {
	testUsers := []string{"dummy", "User-B", "User-C"}
	for _, user := range testUsers {
		sub := "submission_a"

		for i := range 5 {
			if i == 2 {
				sub = "submission_b"
			}

			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("%s/TestGetUserFiles-00%d.c4gh", sub, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				s.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				s.FailNow("failed to update satus of file in database")
			}
		}
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/files?path_prefix=submission_b", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/files", rbac(e), getFiles)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	files := []database.SubmissionFileInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&files)
	assert.NoError(s.T(), err, "failed to list files from DB")
	assert.Equal(s.T(), 3, len(files))
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func testEndpoint(c *gin.Context) {
	c.JSON(200, gin.H{"ok": true})
}

func (s *TestSuite) TestGinLogLevel_Debug() {
	// Capture log output from gin in a buffer
	var buf bytes.Buffer
	gin.DefaultWriter = &buf
	gin.DefaultErrorWriter = &buf

	log.SetLevel(log.DebugLevel)

	// A specific port is enforced here so we don't have a conflict when running the LogLevel_Info test
	Conf.API.Port = 8081
	srv := setup(Conf)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			s.T().Logf("failure: %v", err)
			s.FailNow("failure from API server")
		}
	}()

	// Allow some time for startup
	time.Sleep(500 * time.Millisecond)

	assert.NoError(s.T(), setupJwtAuth())

	// Send a request towards previously started *http.Server
	client := http.Client{Timeout: 5 * time.Second}
	r, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/files", Conf.API.Host, Conf.API.Port), nil)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to create new request instance")
	}

	r.Header.Add("Authorization", "Bearer "+s.Token)
	_, err = client.Do(r) // nolint: bodyclose
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to execute HTTP request")
	}

	// Allow logs to flush
	time.Sleep(500 * time.Millisecond)
	logOutput := buf.String()

	// Remove leading and/or trailing newlines and spaces
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	assert.Greater(s.T(), len(lines), 0)

	// Pick the last produced output from GIN
	logOutput = lines[len(lines)-1]
	assert.Contains(s.T(), logOutput, "[GIN]")

	buf.Reset()
	r, err = http.NewRequest("GET", fmt.Sprintf("http://%s:%d/ready", Conf.API.Host, Conf.API.Port), nil)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to create new request instance for /ready")
	}

	_, err = client.Do(r) // nolint: bodyclose
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to execute HTTP request to /ready")
	}

	// Allow logs to flush
	time.Sleep(500 * time.Millisecond)
	logOutput = buf.String()

	lines = strings.Split(strings.TrimSpace(logOutput), "\n")
	fmt.Println("lines      : ", lines)
	fmt.Println("len(lines) : ", len(lines))
	if len(lines) > 1 {
		assert.NotContains(s.T(), logOutput[len(logOutput)-1], "[GIN]")
	} else {
		s.T().Log("No log output from /ready, considered PASS on log level debug")
	}
}

func (s *TestSuite) TestGinLogLevel_Info() {
	// Capture log output from gin in a buffer
	var buf bytes.Buffer
	gin.DefaultWriter = &buf
	gin.DefaultErrorWriter = &buf

	log.SetLevel(log.InfoLevel)

	// A specific port is enforced here so we don't have a conflict when running the LogLevel_Debug test
	Conf.API.Port = 8082
	srv := setup(Conf)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			s.T().Logf("failure: %v", err)
			s.FailNow("failure from API server")
		}
	}()

	// Allow some time for startup
	time.Sleep(500 * time.Millisecond)

	assert.NoError(s.T(), setupJwtAuth())

	// Send a request towards previously started *http.Server
	client := http.Client{Timeout: 5 * time.Second}
	r, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/files", Conf.API.Host, Conf.API.Port), nil)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to create new request instance")
	}

	r.Header.Add("Authorization", "Bearer "+s.Token)
	_, err = client.Do(r) // nolint: bodyclose
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to execute HTTP request")
	}

	// Allow logs to flush
	time.Sleep(500 * time.Millisecond)
	logOutput := buf.String()

	// Remove leading and/or trailing newlines and spaces
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	assert.Greater(s.T(), len(lines), 0)

	// Pick the last produced output from GIN
	logOutput = lines[len(lines)-1]
	assert.NotContains(s.T(), logOutput, "[GIN]")

	buf.Reset()
	r, err = http.NewRequest("GET", fmt.Sprintf("http://%s:%d/ready", Conf.API.Host, Conf.API.Port), nil)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to create new request instance for /ready")
	}

	_, err = client.Do(r) // nolint: bodyclose
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to execute HTTP request to /ready")
	}

	// Allow logs to flush
	time.Sleep(500 * time.Millisecond)
	logOutput = buf.String()

	lines = strings.Split(strings.TrimSpace(logOutput), "\n")
	fmt.Println("lines      : ", lines)
	fmt.Println("len(lines) : ", len(lines))
	if len(lines) > 1 {
		assert.NotContains(s.T(), logOutput[len(logOutput)-1], "[GIN]")
	} else {
		s.T().Log("No log output from /ready, considered PASS on log level info")
	}
}

func (s *TestSuite) TestRBAC() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/c4gh-keys/list", nil)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/c4gh-keys/list", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)
	assert.Contains(s.T(), string(b), "ok")
}

func (s *TestSuite) TestRBAC_badUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.API.RBACpolicy = []byte(`{"policy":[{"role":"admin","path":"/admin/*","action":"(GET)|(POST)|(PUT)"}],
	"roles":[{"role":"admin","rolebinding":"submission"},
	{"role":"dummy","rolebinding":"submission"}]}`)
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&Conf.API.RBACpolicy))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/list-users", nil)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/admin/list-users", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusUnauthorized, okResponse.StatusCode)
}

func (s *TestSuite) TestRBAC_noToken() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&[]byte{}))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
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
	assert.Equal(s.T(), http.StatusUnauthorized, okResponse.StatusCode)
	assert.Contains(s.T(), string(b), "no access token supplied")
}

func (s *TestSuite) TestRBAC_emptyPolicy() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&[]byte{}))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/files", nil)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/files", rbac(e), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(s.T(), http.StatusUnauthorized, okResponse.StatusCode)
	assert.Contains(s.T(), string(b), "not authorized")
}
func (s *TestSuite) TestIngestFile_WithPayload() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: user, FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", rbac(e), ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/ingest", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestIngestFile_WithFileID() {
	user := "dummy"
	filePath := "/inbox/dummy/file11.c4gh"
	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err)
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	assert.NoError(s.T(), err)
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	assert.NoError(s.T(), err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/file/ingest?fileid="+fileID, nil)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", rbac(e), ingestFile)
	router.ServeHTTP(w, r)

	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/ingest", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestIngestFile_BothFileIDAndPayloadProvided() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"
	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err)
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	assert.NoError(s.T(), err)
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	assert.NoError(s.T(), err)

	w := httptest.NewRecorder()
	payload, _ := json.Marshal(map[string]string{
		"user":     user,
		"filepath": filePath,
	})
	r := httptest.NewRequest("POST", "/file/ingest?fileid="+fileID, bytes.NewBuffer(payload))
	r.Header.Add("Authorization", "Bearer "+s.Token)
	r.Header.Set("Content-Type", "application/json")

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", rbac(e), ingestFile)
	router.ServeHTTP(w, r)

	resp := w.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(s.T(), http.StatusBadRequest, resp.StatusCode)
	assert.Contains(s.T(), string(body), "Both file ID parameter and payload provided")
}

func (s *TestSuite) TestIngestFile_NoFileIDnoPayload() {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/file/ingest", nil)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (s *TestSuite) TestIngestMsgFileID() {
	user := "dummy"
	filePath := "/inbox/dummy_folder/dummyfile.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/file/ingest", nil)

	ingest, corrID, err := ingestMsgFileID(c, fileID)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), user, ingest.User)
	assert.Equal(s.T(), filePath, ingest.FilePath)
	assert.Equal(s.T(), fileID, corrID)
}

func (s *TestSuite) TestIngestMsgFileID_NotFound() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	_, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/file/ingest", nil)

	ingest, corrID, err := ingestMsgFileID(c, "random-id")
	assert.Error(s.T(), err)
	assert.Contains(s.T(), w.Body.String(), "file ID not found")
	assert.Equal(s.T(), http.StatusNotFound, w.Code)
	assert.Empty(s.T(), ingest)
	assert.Empty(s.T(), corrID)
}

func (s *TestSuite) TestIngestMsgFileID_PayloadProvided() {
	user := "dummy"
	filePath := "/inbox/dummy_folder/dummyfile.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	payload, _ := json.Marshal(map[string]string{
		"user":     user,
		"filepath": filePath,
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(payload))
	c.Request.Header.Add("Authorization", "Bearer "+s.Token)

	ingest, corrID, err := ingestMsgFileID(c, fileID)
	assert.Error(s.T(), err)
	assert.Contains(s.T(), w.Body.String(), "Both file ID parameter and payload provided. Choose one")
	assert.Equal(s.T(), http.StatusBadRequest, w.Code)
	assert.Empty(s.T(), ingest)
	assert.Empty(s.T(), corrID)
}

func (s *TestSuite) TestSetAccession() {
	user := "dummy"
	filePath := "/inbox/dummy/file11.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(s.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(s.T(), err)

	fileInfo := database.FileInfo{
		UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(s.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
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
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/accession", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestSetAccession_WrongUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
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
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (s *TestSuite) TestSetAccession_WrongFormat() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/federated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
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
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", rbac(e), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (s *TestSuite) TestCreateDataset() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(s.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(s.T(), err)

	fileInfo := database.FileInfo{
		UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(s.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", rbac(e), createDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	assert.NoError(s.T(), json.Unmarshal(body, &data), "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestCreateDataset_BadFormat() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(s.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(s.T(), err)

	fileInfo := database.FileInfo{
		UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(s.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}")
	assert.NoError(s.T(), err, "got (%v) when marking file as ready", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/federated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", rbac(e), createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(s.T(), err)
	response.Body.Close()

	assert.Equal(s.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(s.T(), string(body), "does not match pattern")
}

func (s *TestSuite) TestCreateDataset_MissingAccessionIDs() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{}, DatasetID: "failure", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(s.T(), err)
	response.Body.Close()

	assert.Equal(s.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(s.T(), string(body), "at least one accessionID is required")
}

func (s *TestSuite) TestCreateDataset_WrongIDs() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(s.T(), err)
	response.Body.Close()

	assert.Equal(s.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(s.T(), string(body), "accession ID not found: ")
}

func (s *TestSuite) TestCreateDataset_WrongUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(s.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(s.T(), err)

	fileInfo := database.FileInfo{
		UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID)
	assert.NoError(s.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(s.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11"}, DatasetID: "API:dataset-01", User: "tester"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.NoError(s.T(), err)
	response.Body.Close()

	assert.Equal(s.T(), http.StatusBadRequest, response.StatusCode)
	assert.Contains(s.T(), string(body), "accession ID owned by other user")
}

func (s *TestSuite) TestReleaseDataset() {
	user := "TestReleaseDataset"
	for i := 0; i < 3; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
		if err != nil {
			s.FailNow("failed to update satus of file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", user, i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_TestReleaseDataset_00", "accession_TestReleaseDataset_01", "accession_TestReleaseDataset_02"}); err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/API:dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestReleaseDataset_NoDataset() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusNotFound, okResponse.StatusCode)
}

func (s *TestSuite) TestReleaseDataset_BadDataset() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/non-existing", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	defer response.Body.Close()
	assert.Equal(s.T(), http.StatusNotFound, response.StatusCode)
}

func (s *TestSuite) TestReleaseDataset_DeprecatedDataset() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 5; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				s.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				s.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"}); err != nil {
		s.FailNow("failed to map files to dataset")
	}

	if err := Conf.API.DB.UpdateDatasetEvent("test-dataset-01", "deprecated", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	Conf.Broker.SchemasPath = "../../schemas/isolated"
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/test-dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", rbac(e), releaseDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	defer response.Body.Close()
	assert.Equal(s.T(), http.StatusBadRequest, response.StatusCode)
}

func (s *TestSuite) TestListActiveUsers() {
	testUsers := []string{"User-A", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 3; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), user)
			if err != nil {
				s.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				s.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_User-A_00", "accession_User-A_01", "accession_User-A_02"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users", rbac(e), listActiveUsers)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	var users []string
	err = json.NewDecoder(okResponse.Body).Decode(&users)
	assert.NoError(s.T(), err, "failed to list users from DB")
	assert.Equal(s.T(), []string{"User-B", "User-C"}, users)
}

func (s *TestSuite) TestListUserFiles() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 5; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				s.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				s.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users/user@example.org/files", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users/:username/files", rbac(e), listUserFiles)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	files := []database.SubmissionFileInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&files)
	assert.NoError(s.T(), err, "failed to list users from DB")
	assert.Equal(s.T(), 2, len(files))
	assert.Contains(s.T(), files[0].AccessionID, "accession_user_example.org_0")
}

func (s *TestSuite) TestListUserFiles_filteredSelection() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		sub := "submission_a"

		for i := range 5 {
			if i == 2 {
				sub = "submission_b"
			}

			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("%s/TestGetUserFiles-00%d.c4gh", sub, i), strings.ReplaceAll(user, "_", "@"))
			if err != nil {
				s.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				s.FailNow("failed to update satus of file in database")
			}
		}
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users/user@example.org/files?path_prefix=submission_b", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users/:username/files", rbac(e), listUserFiles)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	files := []database.SubmissionFileInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&files)
	assert.NoError(s.T(), err, "failed to list user files from DB")
	assert.Equal(s.T(), 3, len(files))
	assert.Contains(s.T(), files[0].Status, "uploaded")
}

func (s *TestSuite) TestAddC4ghHash() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}

	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	// Create a valid request body
	keyhash := schema.C4ghPubKey{
		PubKey:      "LS0tLS1CRUdJTiBDUllQVDRHSCBQVUJMSUMgS0VZLS0tLS0KdWxGRUF6SmZZNEplUEVDZWd3YmJrVVdLNnZ2SE9SWStqMTRGdVpWVnYwND0KLS0tLS1FTkQgQ1JZUFQ0R0ggUFVCTElDIEtFWS0tLS0tCg==",
		Description: "Test key description",
	}
	body, err := json.Marshal(keyhash)
	assert.NoError(s.T(), err)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	// Isert pubkey again and expect error
	resp2, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusConflict, resp2.StatusCode)
	defer resp2.Body.Close()
}

func (s *TestSuite) TestAddC4ghHash_emptyJson() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	// Create an invalid request body
	body := []byte(`{"invalid_json"}`)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (s *TestSuite) TestAddC4ghHash_notBase64() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC model")
	}
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&s.RBAC))
	if err != nil {
		s.T().Logf("failure: %v", err)
		s.FailNow("failed to setup RBAC enforcer")
	}
	r := gin.Default()
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	// Create an invalid request body
	body := []byte(`{"pubkey": "asdkjsahfd=", "decription": ""}`)

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/add", bytes.NewBuffer(body))
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (s *TestSuite) TestListC4ghHashes() {
	assert.NoError(s.T(), Conf.API.DB.AddKeyHash("cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", "this is a test key"), "failed to register key in database")

	expectedResponse := database.C4ghKeyHash{
		Hash:         "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23",
		Description:  "this is a test key",
		CreatedAt:    time.Now().UTC().Format(time.DateTime),
		DeprecatedAt: "",
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	r := gin.Default()
	r.GET("/c4gh-keys/list", listC4ghHashes)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	req, err := http.NewRequest("GET", ts.URL+"/c4gh-keys/list", nil)
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	hashes := []database.C4ghKeyHash{}
	err = json.NewDecoder(resp.Body).Decode(&hashes)
	assert.NoError(s.T(), err, "failed to list users from DB")
	for n, h := range hashes {
		if h.Hash == "cbd8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23" {
			assert.Equal(s.T(), expectedResponse, hashes[n])

			break
		}
	}
}

func (s *TestSuite) TestDeprecateC4ghHash() {
	assert.NoError(s.T(), Conf.API.DB.AddKeyHash("abc8f5cc8d936ce437a52cd9991453839581fc69ee26e0daefde6a5d2660fc23", "this is a deprecation test key"), "failed to register key in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	r := gin.Default()
	r.POST("/c4gh-keys/deprecate/*keyHash", deprecateC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/deprecate/abc8f5cc8d936ce437a52cd9991453839581fc69ee26e0daefde6a5d2660fc23", http.NoBody)
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	// a second time gives an error since the key is alreadu deprecated
	resp2, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusBadRequest, resp2.StatusCode)
	defer resp2.Body.Close()
}

func (s *TestSuite) TestDeprecateC4ghHash_wrongHash() {
	assert.NoError(s.T(), Conf.API.DB.AddKeyHash("abc8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc99", "this is a deprecation test key"), "failed to register key in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	r := gin.Default()
	r.POST("/c4gh-keys/deprecate/*keyHash", deprecateC4ghHash)
	ts := httptest.NewServer(r)
	defer ts.Close()

	client := &http.Client{}
	assert.NoError(s.T(), setupJwtAuth())

	req, err := http.NewRequest("POST", ts.URL+"/c4gh-keys/deprecate/xyz8f5cc8d936ce437a52cd7991453839581fc69ee26e0daefde6a5d2660fc23", http.NoBody)
	assert.NoError(s.T(), err)
	req.Header.Add("Authorization", "Bearer "+s.Token)

	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusBadRequest, resp.StatusCode)
	defer resp.Body.Close()
}

func (s *TestSuite) TestListDatasets() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/dummy/TestGetUserFiles-00%d.c4gh", i), "dummy")
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "dummy", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_dummy_00", "accession_dummy_01", "accession_dummy_02"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_dummy_03", "accession_dummy_04"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets/list", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets/list", listAllDatasets)
	router.GET("/dataset/list", listAllDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(s.T(), err, "failed to list datasets from DB")
	assert.Equal(s.T(), 2, len(datasets))
	assert.Equal(s.T(), "released", datasets[1].Status)
	assert.Equal(s.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (s *TestSuite) TestListUserDatasets() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/user_example.org/TestGetUserFiles-00%d.c4gh", i), strings.ReplaceAll("user_example.org", "_", "@"))
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", "user_example.org", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_user_example.org_03", "accession_user_example.org_04"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets/list/user@example.org", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets/list/:username", listUserDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(s.T(), err, "failed to list datasets from DB")
	assert.Equal(s.T(), 2, len(datasets))
	assert.Equal(s.T(), "released", datasets[1].Status)
	assert.Equal(s.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (s *TestSuite) TestListDatasetsAsUser() {
	for i := 0; i < 5; i++ {
		fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/user_example.org/TestGetUserFiles-00%d.c4gh", i), s.User)
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		stableID := fmt.Sprintf("accession_user_example.org_0%d", i)
		err = Conf.API.DB.SetAccessionID(stableID, fileID)
		if err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-01", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	err = Conf.API.DB.MapFilesToDataset("API:dataset-02", []string{"accession_user_example.org_03", "accession_user_example.org_04"})
	if err != nil {
		s.FailNow("failed to map files to dataset")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "registered", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}
	if err := Conf.API.DB.UpdateDatasetEvent("API:dataset-02", "released", "{}"); err != nil {
		s.FailNow("failed to update dataset event")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/datasets", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/datasets", listDatasets)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	datasets := []database.DatasetInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&datasets)
	assert.NoError(s.T(), err, "failed to list datasets from DB")
	assert.Equal(s.T(), 2, len(datasets))
	assert.Equal(s.T(), "released", datasets[1].Status)
	assert.Equal(s.T(), "API:dataset-01|registered", fmt.Sprintf("%s|%s", datasets[0].DatasetID, datasets[0].Status))
}

func (s *TestSuite) TestReVerifyFile() {
	user := "TestReVerify"
	for i := 0; i < 3; i++ {
		filePath := fmt.Sprintf("/%v/TestReVerify-00%d.c4gh", user, i)
		fileID, err := Conf.API.DB.RegisterFile(filePath, user)
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		if err := Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}"); err != nil {
			s.FailNow("failed to update satus of file in database")
		}
		encSha := sha256.New()
		_, err = encSha.Write([]byte("Checksum"))
		if err != nil {
			s.FailNow("failed to calculate Checksum")
		}

		decSha := sha256.New()
		_, err = decSha.Write([]byte("DecryptedChecksum"))
		if err != nil {
			s.FailNow("failed to calculate DecryptedChecksum")
		}

		fileInfo := database.FileInfo{
			ArchiveChecksum:   fmt.Sprintf("%x", encSha.Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
			DecryptedSize:     948,
			Path:              filePath,
			Size:              1000,
			UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		}
		if err := Conf.API.DB.SetArchived(fileInfo, fileID); err != nil {
			s.FailNow("failed to mark file as Archived")
		}

		if err := Conf.API.DB.SetVerified(fileInfo, fileID); err != nil {
			s.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("accession_%s_0%d", user, i)
		if err := Conf.API.DB.SetAccessionID(stableID, fileID); err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
		if err := Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}"); err != nil {
			s.FailNowf("got (%s) when updating file status: %s", err.Error(), filePath)
		}
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/file/verify/accession_TestReVerify_01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/file/verify/:accession", reVerifyFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/archived", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 1, data.MessagesReady)
}

func (s *TestSuite) TestReVerifyFile_wrongAccession() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/file/verify/accession_TestReVerify_99", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/verify/:accession", reVerifyFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusNotFound, okResponse.StatusCode)
}

func (s *TestSuite) TestReVerifyDataset() {
	user := "TestReVerifyDataset"
	for i := 0; i < 3; i++ {
		filePath := fmt.Sprintf("/%v/TestReVerifyDataset-00%d.c4gh", user, i)
		fileID, err := Conf.API.DB.RegisterFile(filePath, user)
		if err != nil {
			s.FailNow("failed to register file in database")
		}

		if err := Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}"); err != nil {
			s.FailNow("failed to update satus of file in database")
		}
		encSha := sha256.New()
		_, err = encSha.Write([]byte("Checksum"))
		if err != nil {
			s.FailNow("failed to calculate Checksum")
		}

		decSha := sha256.New()
		_, err = decSha.Write([]byte("DecryptedChecksum"))
		if err != nil {
			s.FailNow("failed to calculate DecryptedChecksum")
		}

		fileInfo := database.FileInfo{
			ArchiveChecksum:   fmt.Sprintf("%x", encSha.Sum(nil)),
			DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
			DecryptedSize:     948,
			Path:              filePath,
			Size:              1000,
			UploadedChecksum:  fmt.Sprintf("%x", encSha.Sum(nil)),
		}
		if err := Conf.API.DB.SetArchived(fileInfo, fileID); err != nil {
			s.FailNow("failed to mark file as Archived")
		}

		if err := Conf.API.DB.SetVerified(fileInfo, fileID); err != nil {
			s.FailNow("failed to mark file as Verified")
		}

		stableID := fmt.Sprintf("%s_0%d", user, i)
		if err := Conf.API.DB.SetAccessionID(stableID, fileID); err != nil {
			s.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
		}
		if err := Conf.API.DB.UpdateFileEventLog(fileID, "ready", fileID, "finalize", "{}", "{}"); err != nil {
			s.FailNowf("got (%s) when updating file status: %s", err.Error(), filePath)
		}
	}

	if err := Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"TestReVerifyDataset_00", "TestReVerifyDataset_01", "TestReVerifyDataset_02"}); err != nil {
		s.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dataset/verify/test-dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/dataset/verify/*dataset", reVerifyDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the messages shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/archived", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	assert.NoError(s.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(s.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(s.T(), err, "failed to unmarshal response")
	assert.Equal(s.T(), 3, data.MessagesReady)
}

func (s *TestSuite) TestReVerifyDataset_wrongDatasetName() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(s.T(), setupJwtAuth())
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dataset/verify/wrong_dataset", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+s.Token)

	_, router := gin.CreateTestContext(w)
	router.PUT("/dataset/verify/*dataset", reVerifyDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(s.T(), http.StatusNotFound, okResponse.StatusCode)
}

func (s *TestSuite) TestDownloadFile() {
	mockServerAddress := s.GrpcListener.Listener.Addr().String()
	Conf.API.Grpc.Host, Conf.API.Grpc.Port, err = splitHostPort(mockServerAddress)
	if err != nil {
		s.T().Fatal("failed to split host and port:", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET("/users/:username/file/:fileid", func(c *gin.Context) {
		downloadFile(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Register the file in the database
	fileID, err := Conf.API.DB.RegisterFile(filepath.Base(s.GoodC4ghFile), s.User)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, s.User, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	// Mock request to download the file
	downloadURL := fmt.Sprintf("%s/users/%s/file/%s", ts.URL, s.User, fileID)
	req, err := http.NewRequest("GET", downloadURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Set("C4GH-Public-Key", s.UserKey.PubKeyBase64)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
	assert.Equal(s.T(), "application/octet-stream", resp.Header.Get("Content-Type"))

	// Check the content of the response
	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err, "failed to read response body")
	assert.Contains(s.T(), string(body), "predefined header respons")
}

func (s *TestSuite) TestDownloadFile_badPublicKey() {
	r := gin.Default()
	r.GET("/users/:username/file/:fileid", func(c *gin.Context) {
		downloadFile(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Mock request to download a file
	downloadURL := fmt.Sprintf("%s/users/%s/file/%s", ts.URL, s.User, "d35a6b53-a5d8-4b1b-921f-0a2829b8d2f2")
	req, err := http.NewRequest("GET", downloadURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Set("C4GH-Public-Key", "invalid_key")

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)

	assert.Equal(s.T(), http.StatusBadRequest, resp.StatusCode)
	assert.Contains(s.T(), string(body), "bad public key")
}

func (s *TestSuite) TestDownloadFile_fileIDNotExist() {
	r := gin.Default()
	r.GET("/users/:username/file/:fileid", func(c *gin.Context) {
		downloadFile(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Mock a request to download a file
	downloadURL := fmt.Sprintf("%s/users/%s/file/%s", ts.URL, s.User, "d35a6b53-a5d8-4b1b-921f-0a2829b8d2f2")
	req, err := http.NewRequest("GET", downloadURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Set("C4GH-Public-Key", s.UserKey.PubKeyBase64)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)

	assert.Equal(s.T(), http.StatusNotFound, resp.StatusCode)
	assert.Contains(s.T(), string(body), "failed to retrieve inbox file path")
}

func (s *TestSuite) TestDownloadFile_fileNotExist() {
	r := gin.Default()
	r.GET("/users/:username/file/:fileid", func(c *gin.Context) {
		downloadFile(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Register a file in the database (but don't create the actual file)
	filePath := fmt.Sprintf("/%v/nonexistent.c4gh", s.User)
	fileID, err := Conf.API.DB.RegisterFile(filePath, s.User)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, s.User, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	// Mock request to download the file
	downloadURL := fmt.Sprintf("%s/users/%s/file/%s", ts.URL, s.User, fileID)
	req, err := http.NewRequest("GET", downloadURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Set("C4GH-Public-Key", s.UserKey.PubKeyBase64)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)

	assert.Equal(s.T(), http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(s.T(), string(body), "failed to read inbox file")
}

func (s *TestSuite) TestDownloadFile_badC4ghFile() {
	r := gin.Default()
	r.GET("/users/:username/file/:fileid", func(c *gin.Context) {
		downloadFile(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Register a file in the database (but don't create the actual file)
	fileID, err := Conf.API.DB.RegisterFile(filepath.Base(s.BadC4ghFile), s.User)
	assert.NoError(s.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, s.User, "{}", "{}")
	assert.NoError(s.T(), err, "failed to update satus of file in database")

	// Mock request to download the file
	downloadURL := fmt.Sprintf("%s/users/%s/file/%s", ts.URL, s.User, fileID)
	req, err := http.NewRequest("GET", downloadURL, nil)
	assert.NoError(s.T(), err)
	req.Header.Set("C4GH-Public-Key", s.UserKey.PubKeyBase64)

	client := &http.Client{}
	resp, err := client.Do(req)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)

	assert.Equal(s.T(), http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(s.T(), string(body), "failed to read the start of the file")
}

func (s *TestSuite) TestReencryptHeader() {
	mockServerAddress := s.GrpcListener.Listener.Addr().String()
	log.Debugf("Mock server address: %s", mockServerAddress)
	Conf.API.Grpc.Host, Conf.API.Grpc.Port, err = splitHostPort(mockServerAddress)
	if err != nil {
		s.T().Fatal("failed to split host and port:", err)
	}

	// Call the function under test
	newHeader, err := reencryptHeader(s.FileHeader, s.UserKey.PubKeyBase64)
	if err != nil {
		s.T().Fatal("reencryptHeader failed:", err)
	}

	// Validate the returned header
	expectedHeader := []byte("predefined header response")
	assert.Equal(s.T(), expectedHeader, newHeader, "returned header does not match the expected header")
}

func (s *TestSuite) TestReencryptHeader_failedToConnect() {
	// Mock the server address to an invalid one
	Conf.API.Grpc.Host, Conf.API.Grpc.Port, _ = splitHostPort("localhost:9999")

	newHeader, err := reencryptHeader(s.FileHeader, s.UserKey.PubKeyBase64)
	if err == nil {
		s.T().Fatal("Expected an error due to failed connection, but got nil")
	}
	assert.Equal(s.T(), newHeader, []uint8([]byte(nil)), "expected header to be nil")
	assert.ErrorContains(s.T(), err, "connection refused")
}
