package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var (
	dbPort, grpcPort  int
	certPath, keyPath string
)

type SyncTest struct {
	suite.Suite
	FileHeader        []byte
	UserPublicKeyPath string
}

func TestSyncTestSuite(t *testing.T) {
	suite.Run(t, new(SyncTest))
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

	repKey := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	keyPath, _ = os.MkdirTemp("", "key")
	if err := os.Chmod(keyPath, 0755); err != nil {
		log.Errorf("failed to chmod %s", keyPath)
	}
	// nolint gosec
	if err := os.WriteFile(keyPath+"/c4gh.key", []byte(repKey), 0644); err != nil {
		log.Errorf("failed to generate c4gh key: %s", err.Error())
	}

	certPath, _ = os.MkdirTemp("", "gocerts")
	if err := os.Chmod(certPath, 0755); err != nil {
		log.Errorf("failed to chmod %s", certPath)
	}
	helper.MakeCerts(certPath)

	grpcServer, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sensitive-data-archive",
		Tag:        "v0.3.96",
		Cmd:        []string{"sda-reencrypt"},
		Env: []string{
			"C4GH_FILEPATH=/keys/c4gh.key",
			"C4GH_PASSPHRASE=test",
			"GRPC_CACERT=/certs/ca.crt",
			"GRPC_SERVERCERT=/certs/tls.crt",
			"GRPC_SERVERKEY=/certs/tls.key",
			"LOG_LEVEL=debug",
		},
		ExposedPorts: []string{"50051", "50443"},
		Mounts: []string{
			fmt.Sprintf("%s:/certs", certPath),
			fmt.Sprintf("%s/c4gh.key:/keys/c4gh.key", keyPath),
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	grpcPort, _ = strconv.Atoi(grpcServer.GetPort("50443/tcp"))

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

		query := "SELECT MAX(version) FROM sda.dbschema_version;"
		var dbVersion int

		return db.QueryRow(query).Scan(&dbVersion)
	}); err != nil {
		if err := pool.Purge(grpcServer); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	log.Println("starting tests")
	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(grpcServer); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	pvo := docker.PruneVolumesOptions{Filters: make(map[string][]string), Context: context.Background()}
	if _, err := pool.Client.PruneVolumes(pvo); err != nil {
		log.Fatalf("could not prune docker volumes: %s", err.Error())
	}
}

func (suite *SyncTest) SetupSuite() {
	viper.Set("log.level", "debug")
	viper.Set("archive.type", "posix")
	viper.Set("archive.location", "../../dev_utils")
	viper.Set("sync.destination.type", "posix")
	viper.Set("sync.destination.location", "../../dev_utils")

	viper.Set("broker.host", "localhost")
	viper.Set("broker.port", 123)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.queue", "test")
	viper.Set("db.host", "localhost")
	viper.Set("db.port", dbPort)
	viper.Set("db.user", "postgres")
	viper.Set("db.password", "rootpasswd")
	viper.Set("db.database", "sda")
	viper.Set("db.sslmode", "disable")
	viper.Set("sync.remote.host", "http://remote.example")
	viper.Set("sync.remote.user", "user")
	viper.Set("sync.remote.password", "pass")

	key := "-----BEGIN CRYPT4GH ENCRYPTED PRIVATE KEY-----\nYzRnaC12MQAGc2NyeXB0ABQAAAAAEna8op+BzhTVrqtO5Rx7OgARY2hhY2hhMjBfcG9seTEzMDUAPMx2Gbtxdva0M2B0tb205DJT9RzZmvy/9ZQGDx9zjlObj11JCqg57z60F0KhJW+j/fzWL57leTEcIffRTA==\n-----END CRYPT4GH ENCRYPTED PRIVATE KEY-----"
	suite.UserPublicKeyPath, _ = os.MkdirTemp("", "key")
	err := os.WriteFile(suite.UserPublicKeyPath+"/c4gh.key", []byte(key), 0600)
	assert.NoError(suite.T(), err)

	viper.Set("c4gh.filepath", suite.UserPublicKeyPath+"/c4gh.key")
	viper.Set("c4gh.passphrase", "test")

	pubKey := "-----BEGIN CRYPT4GH PUBLIC KEY-----\nuQO46R56f/Jx0YJjBAkZa2J6n72r6HW/JPMS4tfepBs=\n-----END CRYPT4GH PUBLIC KEY-----"
	err = os.WriteFile(suite.UserPublicKeyPath+"/c4gh.pub", []byte(pubKey), 0600)
	assert.NoError(suite.T(), err)
	viper.Set("c4gh.syncPubKeyPath", suite.UserPublicKeyPath+"/c4gh.pub")
}

func (suite *SyncTest) TearDownSuite() {
	os.RemoveAll(suite.UserPublicKeyPath)
	os.RemoveAll(certPath)
	os.RemoveAll(keyPath)
}

func (suite *SyncTest) TestSendPOST() {
	r := http.NewServeMux()
	r.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		username, _, ok := r.BasicAuth()
		if ok && username != "test" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	ts := httptest.NewServer(r)
	defer ts.Close()

	conf = &config.Config{}
	conf.Sync = config.Sync{
		RemoteHost:     ts.URL,
		RemoteUser:     "test",
		RemotePassword: "test",
	}
	syncJSON := []byte(`{"user":"test.user@example.com", "filepath": "inbox/user/file1.c4gh"}`)
	err := sendPOST(syncJSON, "ingest")
	assert.NoError(suite.T(), err)

	conf.Sync = config.Sync{
		RemoteHost:     ts.URL,
		RemoteUser:     "foo",
		RemotePassword: "bar",
	}
	assert.EqualError(suite.T(), sendPOST(syncJSON, "ingest"), "401 Unauthorized")
}

func (suite *SyncTest) TestReencryptHeader() {
	header, _ := hex.DecodeString("637279707434676801000000010000006c000000000000007ca283608311dacfc32703a3cc9a2b445c9a417e036ba5943e233cfc65a1f81fdcc35036a584b3f95759114f584d1e81e8cf23a9b9d1e77b9e8f8a8ee8098c2a3e9270fe6872ef9d1c948caf8423efc7ce391081da0d52a49b1e6d0706f267d6140ff12b")
	viper.Set("reencrypt.host", "localhost")
	viper.Set("reencrypt.port", grpcPort)
	viper.Set("reencrypt.caCert", certPath+"/ca.crt")
	viper.Set("reencrypt.clientCert", certPath+"/tls.crt")
	viper.Set("reencrypt.clientKey", certPath+"/tls.key")
	conf, err := config.NewConfig("sync")
	assert.NoError(suite.T(), err)

	newHeader, err := reencryptHeader(conf.Sync.Reencrypt, header, conf.Sync.PublicKey)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "crypt4gh", string(newHeader[:8]))

	hr := bytes.NewReader(newHeader)
	fileData, _ := hex.DecodeString("e046718f01d52c626276ce5931e10afd99330c4679b3e2a43fdf18146e85bae8eaee83")
	fileStream := io.MultiReader(hr, bytes.NewReader(fileData))

	keyFile, err := os.Open(keyPath + "/c4gh.key")
	assert.NoError(suite.T(), err)

	key, err := keys.ReadPrivateKey(keyFile, []byte("test"))
	assert.NoError(suite.T(), err)

	c4gh, err := streaming.NewCrypt4GHReader(fileStream, key, nil)
	assert.NoError(suite.T(), err)

	data, err := io.ReadAll(c4gh)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "content", string(data))
}
