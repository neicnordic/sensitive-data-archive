package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"

	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
)

var dbPort, mqPort int

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}
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

	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(rabbitmq); err != nil {
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
	Conf.API.DB, err = database.NewDB(Conf.Database)
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
	Conf.API.DB, err = database.NewDB(Conf.Database)
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
