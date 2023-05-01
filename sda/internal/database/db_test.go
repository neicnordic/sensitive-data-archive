package database

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DatabaseTests struct {
	suite.Suite
	dbConf DBConf
}

var dbPort int

func TestDatabaseTestSuite(t *testing.T) {
	suite.Run(t, new(DatabaseTests))
}

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
		Repository: "ghcr.io/neicnordic/sensitive-data-archive",
		Tag:        "PR84-postgres",
		Env: []string{
			"POSTGRES_PASSWORD=rootpasswd",
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
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
}

func (suite *DatabaseTests) SetupTest() {
	// Database connection variables
	suite.dbConf = DBConf{
		Host:       "127.0.0.1",
		Port:       dbPort,
		User:       "postgres",
		Password:   "rootpasswd",
		Database:   "sda",
		CACert:     "",
		SslMode:    "disable",
		ClientCert: "",
		ClientKey:  "",
	}

}

func (suite *DatabaseTests) TearDownTest() {}

// TestNewSDAdb tests creation of new database connections, as well as fetching
// of the database schema version.
func (suite *DatabaseTests) TestNewSDAdb() {

	// test working database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.Nil(suite.T(), err, "got %v when creating new connection", err)

	db.Close()

	// test wrong credentials
	wrongConf := DBConf{
		Host:       "localhost",
		Port:       dbPort,
		User:       "hacker",
		Password:   "password123",
		Database:   "lega",
		CACert:     "",
		SslMode:    "disable",
		ClientCert: "",
		ClientKey:  "",
	}

	_, err = NewSDAdb(wrongConf)
	assert.NotNil(suite.T(), err, "connection allowed with wrong credentials")

}

// TestConnect tests creation of new database connections
func (suite *DatabaseTests) TestConnect() {

	// test connecting to a database
	db := SDAdb{DB: nil, Version: -1, Config: suite.dbConf}

	err := db.Connect()
	assert.Nil(suite.T(), err, "failed connecting: %s", err)

	// test that nothing happens if you connect when already connected
	err = db.Connect()
	assert.Nil(suite.T(), err, "Connect() should return nil when called on an"+
		" already open connection: %s", err)

	// test querying a closed connection
	db.Close()
	query := "SELECT MAX(version) FROM local_ega.dbschema_version"
	var dbVersion = -1
	err = db.DB.QueryRow(query).Scan(&dbVersion)
	assert.NotNil(suite.T(), err, "query possible on closed connection")

	// test reconnection by using getVersion()
	_, err = db.getVersion()
	assert.Nil(suite.T(), err, "failed reconnecting: %s", err)

	db.Close()

	// test wrong credentials
	wrongConf := DBConf{
		Host:       "localhost",
		Port:       5432,
		User:       "hacker",
		Password:   "password123",
		Database:   "lega",
		CACert:     "",
		SslMode:    "disable",
		ClientCert: "",
		ClientKey:  "",
	}

	db.Config = wrongConf
	err = db.Connect()
	assert.NotNil(suite.T(), err, "connection allowed with wrong credentials")

}

// TestClose tests that the connection is properly closed
func (suite *DatabaseTests) TestClose() {

	// test working database connection
	db, err := NewSDAdb(suite.dbConf)
	assert.Nil(suite.T(), err, "got %v when creating new connection", err)

	db.Close()

	// check that we can't do queries on a closed connection
	query := "SELECT MAX(version) FROM local_ega.dbschema_version"
	var dbVersion = -1
	err = db.DB.QueryRow(query).Scan(&dbVersion)
	assert.NotNil(suite.T(), err, "query possible on closed connection")

	// check that nothing happens if Close is called on a closed connection
	db.Close()
	assert.NotPanics(suite.T(), db.Close,
		"Close paniced when called on closed connection")
}
