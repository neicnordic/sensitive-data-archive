package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/database"
	log "github.com/sirupsen/logrus"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DatabaseTests struct {
	suite.Suite

	db             database.Database
	verificationDB *sql.DB
}

var dbPort int

func TestDatabaseTestSuite(t *testing.T) {
	suite.Run(t, new(DatabaseTests))
}

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		os.Exit(m.Run())
	}

	_, b, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(b), "../../../../")

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
		log.Println("waiting for docker postgres database to be ready")
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			log.Println(err)

			return err
		}
		query := "SELECT MAX(version) FROM sda.dbschema_version"
		var dbVersion int

		return db.QueryRow(query).Scan(&dbVersion)
	}); err != nil {
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func (ts *DatabaseTests) SetupTest() {
	dbConf := &dbConfig{
		host:         "127.0.0.1",
		port:         dbPort,
		user:         "postgres",
		password:     "rootpasswd",
		databaseName: "sda",
		schema:       "sda",
		cACert:       "",
		sslMode:      "disable",
		clientCert:   "",
		clientKey:    "",
	}
	var err error
	// test working database connection
	ts.db, err = NewPostgresSQLDatabase(
		Host(dbConf.host),
		Port(dbConf.port),
		User(dbConf.user),
		Password(dbConf.password),
		DatabaseName(dbConf.databaseName),
		Schema(dbConf.schema),
		CACert(dbConf.cACert),
		SslMode(dbConf.sslMode),
		ClientCert(dbConf.clientCert),
		ClientKey(dbConf.clientKey),
	)
	if err != nil {
		ts.FailNow("Could not connect to Postgres: %s", err)
	}

	ts.verificationDB, err = sql.Open("postgres", dbConf.dataSourceName())
	if err != nil {
		ts.FailNow(fmt.Sprintf("failed to connect to database: %v", err))
	}

	if err := ts.verificationDB.Ping(); err != nil {
		ts.FailNow(fmt.Sprintf("failed to connect to database: %v", err))
	}

	assert.Nil(ts.T(), err, "got %v when creating new connection", err)
	_, err = ts.verificationDB.Exec("TRUNCATE sda.files, sda.encryption_keys CASCADE")
	assert.NoError(ts.T(), err)
}

func (ts *DatabaseTests) TearDownTest() {
	if ts.db != nil {
		ts.NoError(ts.db.Close())
	}
	if ts.verificationDB != nil {
		ts.NoError(ts.verificationDB.Close())
	}
}

// TestWrongPassword tests creation of new database connections with the wrong password
func (ts *DatabaseTests) TestWrongPassword() {
	// test wrong credentials
	_, err := NewPostgresSQLDatabase(
		Host("localhost"),
		Port(dbPort),
		User("hacker"),
		Password("password123"),
		DatabaseName("lega"),
		CACert(""),
		SslMode("disable"),
		ClientCert(""),
		ClientKey(""),
	)
	assert.NotNil(ts.T(), err, "connection allowed with wrong credentials")
}
