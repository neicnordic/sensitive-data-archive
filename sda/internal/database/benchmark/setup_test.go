package benchmark

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	_ "github.com/lib/pq"
)

var testDB *sql.DB
var dbPort int

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		os.Exit(m.Run())
	}

	_, thisFile, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(thisFile), "..", "..", "..", "..")

	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("pool: %s", err)
	}
	if err := pool.Client.Ping(); err != nil {
		log.Fatalf("docker ping: %s", err)
	}

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
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("start postgres: %s", err)
	}

	hostPort := postgres.GetHostPort("5432/tcp")
	dbPort, _ = strconv.Atoi(postgres.GetPort("5432/tcp"))
	dbURL := fmt.Sprintf("postgres://postgres:rootpasswd@%s/sda?sslmode=disable", hostPort)

	pool.MaxWait = 120 * time.Second
	if err := pool.Retry(func() error {
		db, err := sql.Open("postgres", dbURL)
		if err != nil {
			return err
		}
		defer db.Close()
		var v int
		return db.QueryRow("SELECT MAX(version) FROM sda.dbschema_version").Scan(&v)
	}); err != nil {
		_ = pool.Purge(postgres)
		log.Fatalf("postgres not ready: %s", err)
	}

	testDB, err = sql.Open("postgres", dbURL)
	if err != nil {
		_ = pool.Purge(postgres)
		log.Fatalf("open db: %s", err)
	}
	testDB.SetMaxOpenConns(10)
	testDB.SetMaxIdleConns(10)

	code := m.Run()

	_ = testDB.Close()
	_ = pool.Purge(postgres)
	os.Exit(code)
}
