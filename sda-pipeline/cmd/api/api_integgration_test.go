package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"sda-pipeline/internal/broker"
	"sda-pipeline/internal/config"
	"sda-pipeline/internal/database"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

func TestShutdown(t *testing.T) {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       5672,
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
		Port:     5432,
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
	r := mux.NewRouter()
	r.HandleFunc("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:       "localhost",
		Port:       5672,
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
		Port:     5432,
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

	// close the connection to force a reconneciton
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
