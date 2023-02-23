package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DatabaseTests struct {
	suite.Suite
	dbConf DBConf
}

func TestDatabaseTestSuite(t *testing.T) {
	suite.Run(t, new(DatabaseTests))
}

func (suite *DatabaseTests) SetupTest() {

	// check if we're in a docker container (the /.dockerenv file is available
	// in all docker containers)
	dbHost := "localhost"
	_, err := os.Stat("/.dockerenv")
	if err == nil {
		dbHost = "db"
	}

	// Database connection variables
	suite.dbConf = DBConf{
		Host:       dbHost,
		Port:       5432,
		User:       "lega_in",
		Password:   "lega_in",
		Database:   "lega",
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
		Port:       5432,
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
