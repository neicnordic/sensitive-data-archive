package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	re "github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var dbPort, mqPort int

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
		Tag:        "15.4-alpine3.17",
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

		query := "SELECT MAX(version) FROM sda.dbschema_version;"
		var dbVersion int

		return db.QueryRow(query).Scan(&dbVersion)
	}); err != nil {
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sensitive-data-archive",
		Tag:        "v3.0.0-rabbitmq",
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
	brokerAPI := rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+brokerAPI+"/api/queues/sda/", http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth("guest", "guest")

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil || res.StatusCode != 200 {
			return err
		}
		_ = res.Body.Close()

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

	log.Println("starting tests")
	code := m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(rabbitmq); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

type TestSuite struct {
	suite.Suite
	app            RotateKey
	fileID         string
	privateKeyList []*[32]byte
}
type server struct {
	re.UnimplementedReencryptServer
	c4ghPrivateKeyList []*[32]byte
}

func TestRotateKeyTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (ts *TestSuite) SetupSuite() {
	ts.app.Conf = &config.Config{}
	ts.app.Conf.Broker.SchemasPath = "../../schemas/isolated"
	var err error
	ts.app.DB, err = database.NewSDAdb(database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	})
	if err != nil {
		ts.FailNow("Failed to create DB connection")
	}

	ts.app.MQ, err = broker.NewMQ(broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	})
	if err != nil {
		ts.T().Log(err.Error())
		ts.FailNow("Failed to create MQ connection")
	}

	publicKey, _, err := keys.GenerateKeyPair()
	if err != nil {
		ts.FailNow("Failed to create new c4gh keypair")
	}

	for i, kh := range []string{"79f2f4dd9cd9435743d5e8ef3d0da55d64437055e89cfa5531395abf8857bd63", hex.EncodeToString(publicKey[:])} {
		if err := ts.app.DB.AddKeyHash(kh, fmt.Sprintf("key num: %d", i)); err != nil {
			ts.FailNow("failed to register a public key")
		}
	}

	ts.app.Conf.RotateKey.PublicKey = &publicKey

	ts.fileID, err = ts.app.DB.RegisterFile(nil, "rotate-key-test/data.c4gh", "tester_example.org")
	if err != nil {
		ts.FailNow("Failed to register file in DB")
	}
	for _, status := range []string{"uploaded", "archived", "verified"} {
		if err = ts.app.DB.UpdateFileEventLog(ts.fileID, status, "tester_example.org", "{}", "{}"); err != nil {
			ts.FailNow("Failed to set status of file in DB")
		}
	}
	if err := ts.app.DB.SetKeyHash("79f2f4dd9cd9435743d5e8ef3d0da55d64437055e89cfa5531395abf8857bd63", ts.fileID); err != nil {
		ts.FailNow("Failed to set key hash of file in DB")
	}
	if err := ts.app.DB.StoreHeader([]byte("637279707434676801000000010000006c000000000000004f6ae97503ac19b6316cb3330ea4e55e0fa98ed7342afc79deec64606aa33a587e78743695f3be5d5b9d0f386c2b66aefb06de07c506eccec4910455d75f54ce6324b98b4dd35dcc6c0684bbf8a05fb5c2976f540dbbbc95646c2e55ec52c5833115e5659"), ts.fileID); err != nil {
		ts.FailNow("Failed to store header of file in DB")
	}

	fileInfo := database.FileInfo{
		ArchiveChecksum:   "239729e2f471a02f8b43374fa58ea2d3a85ec93874b58696030b4af804c32f36",
		DecryptedChecksum: "9aa63cfe45c560c8f16dde4b002a3fe38afa69801df6a6e266b757ab6aace2d8",
		DecryptedSize:     34,
		Path:              ts.fileID,
		Size:              59,
	}
	if err := ts.app.DB.SetVerified(fileInfo, ts.fileID); err != nil {
		ts.FailNow("Failed to store header of file in DB")
	}

	lis, err := net.Listen("tcp", "localhost:")
	if err != nil {
		log.Errorf("failed to create listener: %v", err)
		ts.T().FailNow()
	}
	reHost, rePort, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		ts.T().FailNow()
	}
	go func() {
		var opts []grpc.ServerOption
		s := grpc.NewServer(opts...)
		re.RegisterReencryptServer(s, &server{c4ghPrivateKeyList: ts.privateKeyList})
		reflection.Register(s)
		if err := s.Serve(lis); err != nil {
			log.Errorf("failed to start GRPC server: %v", err)
			ts.T().Fail()
		}
	}()

	rePortInt, err := strconv.Atoi(rePort)
	if err != nil {
		ts.T().FailNow()
	}

	ts.app.Conf.RotateKey.Grpc = config.Grpc{
		Host:    reHost,
		Port:    rePortInt,
		Timeout: 30,
	}

	ts.T().Log("suite setup completed")
}

// ReencryptHeader serves a mock response since we don't need to test the actual reencryption
func (s *server) ReencryptHeader(ctx context.Context, req *re.ReencryptRequest) (*re.ReencryptResponse, error) {
	// Mock response based on your needs
	if req.Publickey == "phail" {
		return &re.ReencryptResponse{}, errors.New("bad error")
	}

	mockedResponse := &re.ReencryptResponse{
		Header: []byte("predefined header response"),
	}

	return mockedResponse, nil
}

func (ts *TestSuite) TestReEncryptHeader() {
	for _, test := range []struct {
		corrID        string
		expectedError error
		expectedMgs   string
		expectedRes   string
		fileID        string
		testName      string
	}{
		{
			testName:      "ingested file",
			expectedError: nil,
			expectedMgs:   "",
			expectedRes:   "ack",
			fileID:        ts.fileID,
		},
		{
			testName:      "un-ingested file",
			expectedError: errors.New("sql: no rows in result set"),
			expectedMgs:   fmt.Sprintf("failed to get keyhash for file with file-id: %s", ts.fileID),
			expectedRes:   "ackSendToError",
			corrID:        uuid.New().String(),
			fileID:        ts.fileID,
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			res, msg, err := ts.app.reEncryptHeader(test.corrID, test.fileID)
			assert.Equal(t, res, test.expectedRes)
			assert.Equal(t, msg, test.expectedMgs)
			assert.Equal(t, err, test.expectedError)
		})
	}
}
