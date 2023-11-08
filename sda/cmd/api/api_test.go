package main

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

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
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/spf13/viper"
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
	PrivateKey  *rsa.PrivateKey
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")

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

	conf := config.Config{}
	conf.API.Host = "localhost"
	conf.API.Port = 8080
	server := setup(&conf)

	assert.Equal(suite.T(), "localhost:8080", server.Addr)

	suite.Path = "/tmp/keys/"
	suite.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	privpath, pubpath, err := helper.MakeFolder(suite.Path)
	assert.NoError(suite.T(), err)
	suite.PrivatePath = privpath
	suite.PublicPath = pubpath
	err = helper.CreateRSAkeys(privpath, pubpath)
	assert.NoError(suite.T(), err)

}

func TestDatabasePingCheck(t *testing.T) {
	database := database.SDAdb{}
	assert.Error(t, checkDB(&database, 1*time.Second), "nil DB should fail")

	database.DB, _, err = sqlmock.New()
	assert.NoError(t, err)
	assert.NoError(t, checkDB(&database, 1*time.Second), "ping should succeed")
}

func (suite *TestSuite) TestGetUserFromURLToken() {
	// Get key set from oidc
	auth := userauth.NewValidateFromToken(jwk.NewSet())
	jwtpubkeyurl := fmt.Sprintf("http://localhost:%d/jwk", OIDCport)
	err := auth.FetchJwtPubKeyURL(jwtpubkeyurl)
	assert.NoError(suite.T(), err, "failed to fetch remote JWK")
	assert.Equal(suite.T(), 3, auth.Keyset.Len())

	// Get token from oidc
	token_url := fmt.Sprintf("http://localhost:%d/tokens", OIDCport)
	resp, err := http.Get(token_url)
	assert.NoError(suite.T(), err, "Error getting token from oidc")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(suite.T(), err, "Error reading token from oidc")

	var tokens []string
	err = json.Unmarshal(body, &tokens)
	assert.NoError(suite.T(), err, "Error unmarshalling token")
	assert.GreaterOrEqual(suite.T(), len(tokens), 1)

	rawkey := tokens[0]

	// Call get files api
	url := "localhost:8080/files"
	method := "GET"
	r, err := http.NewRequest(method, url, nil)

	assert.NoError(suite.T(), err)

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %v", rawkey))

	user, err := getUserFromToken(r, auth)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "requester@demo.org", user)

}

func (suite *TestSuite) TestGetUserFromPathToken() {
	c := &config.Config{}
	ServerConf := config.ServerConfig{}
	ServerConf.Jwtpubkeypath = suite.PublicPath
	c.Server = ServerConf

	Conf = c

	auth := userauth.NewValidateFromToken(jwk.NewSet())
	err := auth.ReadJwtPubKeyPath(Conf.Server.Jwtpubkeypath)
	assert.NoError(suite.T(), err, "Error while getting key "+Conf.Server.Jwtpubkeypath)

	url := "localhost:8080/files"
	method := "GET"
	r, err := http.NewRequest(method, url, nil)
	assert.NoError(suite.T(), err)

	// Valid token
	prKeyParsed, err := helper.ParsePrivateRSAKey(suite.PrivatePath, "/rsa")
	assert.NoError(suite.T(), err)
	token, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)
	r.Header.Add("Authorization", "Bearer "+token)

	user, err := getUserFromToken(r, auth)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "dummy", user)

	// Token without authorization header
	r.Header.Del("Authorization")

	user, err = getUserFromToken(r, auth)
	assert.EqualError(suite.T(), err, "failed to get parse token: no access token supplied")

	assert.Equal(suite.T(), "", user)

	// Token without issuer
	NoIssuer := helper.DefaultTokenClaims
	NoIssuer["iss"] = ""
	log.Printf("Noissuer %v with iss %v", NoIssuer, NoIssuer["iss"])
	token, err = helper.CreateRSAToken(prKeyParsed, "RS256", NoIssuer)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	r.Header.Add("Authorization", "Bearer "+token)

	user, err = getUserFromToken(r, auth)
	assert.EqualError(suite.T(), err, "failed to get parse token: failed to get issuer from token (<nil>)")
	assert.Equal(suite.T(), "", user)

}
