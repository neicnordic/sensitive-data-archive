package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwk"

	log "github.com/sirupsen/logrus"
)

var Conf *config.Config
var err error
var auth *userauth.ValidateFromToken

func main() {
	Conf, err = config.NewConfig("api")
	if err != nil {
		log.Fatal(err)
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	if err != nil {
		log.Fatal(err)
	}

	auth = userauth.NewValidateFromToken(jwk.NewSet())
	if Conf.Server.Jwtpubkeyurl != "" {
		if err := auth.FetchJwtPubKeyURL(Conf.Server.Jwtpubkeyurl); err != nil {
			log.Panicf("Error while getting key %s: %v", Conf.Server.Jwtpubkeyurl, err)
		}
	}
	if Conf.Server.Jwtpubkeypath != "" {
		if err := auth.ReadJwtPubKeyPath(Conf.Server.Jwtpubkeypath); err != nil {
			log.Panicf("Error while getting key %s: %v", Conf.Server.Jwtpubkeypath, err)
		}
	}

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sigc
		shutdown()
		os.Exit(0)
	}()

	srv := setup(Conf)

	if Conf.API.ServerCert != "" && Conf.API.ServerKey != "" {
		log.Infof("Web server is ready to receive connections at https://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServeTLS(Conf.API.ServerCert, Conf.API.ServerKey); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	} else {
		log.Infof("Web server is ready to receive connections at http://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServe(); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	}
}

func setup(config *config.Config) *http.Server {

	r := gin.Default()

	r.GET("/ready", readinessResponse)
	r.GET("/files", getFiles)

	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	srv := &http.Server{
		Addr:              config.API.Host + ":" + fmt.Sprint(config.API.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      20 * time.Second,
	}

	return srv
}

func shutdown() {
	defer Conf.API.MQ.Channel.Close()
	defer Conf.API.MQ.Connection.Close()
	defer Conf.API.DB.Close()
}

func readinessResponse(c *gin.Context) {
	statusCode := http.StatusOK

	if Conf.API.MQ.Connection.IsClosed() {
		statusCode = http.StatusServiceUnavailable
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	if Conf.API.MQ.Channel.IsClosed() {
		statusCode = http.StatusServiceUnavailable
		Conf.API.MQ.Connection.Close()
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	if DBRes := checkDB(Conf.API.DB, 5*time.Millisecond); DBRes != nil {
		log.Debugf("DB connection error :%v", DBRes)
		Conf.API.DB.Reconnect()
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, "")
}

func checkDB(database *database.SDAdb, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if database.DB == nil {
		return fmt.Errorf("database is nil")
	}

	return database.DB.PingContext(ctx)
}

// getFiles returns the files from the database for a specific user
func getFiles(c *gin.Context) {

	c.Writer.Header().Set("Content-Type", "application/json")
	// Get user ID to extract all files
	// userID, err := getUserFromToken(c.Request)
	token, err := auth.Authenticate(c.Request)
	if err != nil {
		// something went wrong with user token
		c.JSON(401, err.Error())

		return
	}

	files, err := Conf.API.DB.GetUserFiles(token.Subject())
	if err != nil {
		// something went wrong with querying or parsing rows
		c.JSON(502, err.Error())

		return
	}

	// Return response
	c.JSON(200, files)
}
