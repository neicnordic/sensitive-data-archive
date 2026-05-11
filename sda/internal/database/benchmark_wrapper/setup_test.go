// Package benchmark_wrapper benches Karl's public database.Database wrapper
// end-to-end (constructor + prepared statements + method dispatch), as a
// validation run for variant 01c vs our 01b adapter in BENCH_ADAPTER=prep
// mode. Both fixtures boot the same dockertest postgres at the same Karl
// SHA (1424461f); the only difference is the code path under test.
package benchmark_wrapper

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

	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
)

// rawDB is a plain *sql.DB used only for fixture setup (seed + truncate).
// This matches the benchmark/ package pattern so fixture cost is identical
// and we do not leak raw setup into Karl's prep path.
var rawDB *sql.DB

// karlDB is Karl's full wrapper: NewPostgresSQLDatabase prepares every
// query in his global `queries` map at startup. Bench methods dispatch
// through the database.Database interface.
var karlDB database.Database

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

	pg, err := pool.RunWithOptions(&dockertest.RunOptions{
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

	hostPort := pg.GetHostPort("5432/tcp")
	dbPort, _ := strconv.Atoi(pg.GetPort("5432/tcp"))
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
		_ = pool.Purge(pg)
		log.Fatalf("postgres not ready: %s", err)
	}

	rawDB, err = sql.Open("postgres", dbURL)
	if err != nil {
		_ = pool.Purge(pg)
		log.Fatalf("open rawDB: %s", err)
	}
	rawDB.SetMaxOpenConns(10)
	rawDB.SetMaxIdleConns(10)

	// Boot Karl's wrapper via his public constructor. Karl does not expose
	// pool tuning; the internal *sql.DB keeps Go's defaults (MaxOpenConns=0
	// unlimited, MaxIdleConns=2). The microbench is single-threaded, so a
	// lone connection is reused for every iteration and the pool-size
	// difference vs the adapter bench cannot bias per-call timings.
	karlDB, err = postgres.NewPostgresSQLDatabase(
		postgres.Host("localhost"),
		postgres.Port(dbPort),
		postgres.User("postgres"),
		postgres.Password("rootpasswd"),
		postgres.DatabaseName("sda"),
		postgres.Schema("sda"),
		postgres.SslMode("disable"),
	)
	if err != nil {
		_ = pool.Purge(pg)
		log.Fatalf("new postgres wrapper: %s", err)
	}

	code := m.Run()

	_ = karlDB.Close()
	_ = rawDB.Close()
	_ = pool.Purge(pg)
	os.Exit(code)
}
