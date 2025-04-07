package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

	log "github.com/sirupsen/logrus"
)

// Export Conf so we can access it in the other modules
var Conf *config.Config

func main() {
	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Create a function to handle panic and exit gracefully
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Could not recover from %v", err)
			log.Fatal("Could not recover, exiting")
		}
	}()

	conf, err := config.NewConfig("s3inbox")
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	tlsProxy, err := config.TLSConfigProxy(conf)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	sdaDB, err := database.NewSDAdb(conf.Database)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	if sdaDB.Version < 4 {
		log.Error("database schema v4 is required")
		sigc <- syscall.SIGINT
		panic(err)
	}

	log.Debugf("Connected to sda-db (v%v)", sdaDB.Version)

	s3, err := storage.NewS3Client(conf.Inbox.S3)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	err = storage.CheckS3Bucket(conf.Inbox.S3.Bucket, s3)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	messenger, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	go func() {
		<-sigc
		sdaDB.Close()
		messenger.Channel.Close()
		messenger.Connection.Close()
		os.Exit(1)
	}()
	auth := userauth.NewValidateFromToken(jwk.NewSet())
	// Load keys for JWT verification
	if Conf.Server.Jwtpubkeyurl != "" {
		if err := auth.FetchJwtPubKeyURL(conf.Server.Jwtpubkeyurl); err != nil {
			log.Panicf("Error while getting key %s: %v", Conf.Server.Jwtpubkeyurl, err)
		}
	}
	if conf.Server.Jwtpubkeypath != "" {
		if err := auth.ReadJwtPubKeyPath(conf.Server.Jwtpubkeypath); err != nil {
			log.Panicf("Error while getting key %s: %v", conf.Server.Jwtpubkeypath, err)
		}
	}
	m := mux.NewRouter()
	proxy := NewProxy(conf.Inbox.S3, auth, messenger, sdaDB, tlsProxy)
	m.HandleFunc("/", proxy.CheckHealth).Methods("HEAD")
	m.HandleFunc("/health", proxy.CheckHealth)
	m.PathPrefix("/").Handler(proxy)

	server := &http.Server{
		Addr:              ":8000",
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 30 * time.Second,
		Handler:           m,
	}

	if conf.Server.Cert != "" && conf.Server.Key != "" {
		if err := server.ListenAndServeTLS(conf.Server.Cert, conf.Server.Key); err != nil {
			panic(err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil {
			panic(err)
		}
	}
}
