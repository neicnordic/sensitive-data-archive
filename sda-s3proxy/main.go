package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	common "github.com/neicnordic/sda-common/database"
	log "github.com/sirupsen/logrus"
)

// Export Conf so we can access it in the other modules
var Conf *Config

func main() {
	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Create a function to handle panic and exit gracefully
	defer func() {
		if err := recover(); err != nil {
			log.Fatal("Could not recover, exiting")
		}
	}()

	c, err := NewConfig()
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	Conf = c

	tlsBroker, err := TLSConfigBroker(Conf)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}
	tlsProxy, err := TLSConfigProxy(Conf)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	sdaDB, err := common.NewSDAdb(Conf.DB)
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

	err = checkS3Bucket(Conf.S3)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	messenger, err := NewAMQPMessenger(Conf.Broker, tlsBroker)
	if err != nil {
		log.Error(err)
		sigc <- syscall.SIGINT
		panic(err)
	}

	log.Debug("messenger acquired ", messenger)

	go func() {
		<-sigc
		sdaDB.Close()
		messenger.channel.Close()
		messenger.connection.Close()
		os.Exit(1)
	}()
	var pubkeys map[string][]byte
	auth := NewValidateFromToken(pubkeys)
	auth.pubkeys = make(map[string][]byte)
	// Load keys for JWT verification
	if Conf.Server.jwtpubkeyurl != "" {
		if err := auth.getjwtpubkey(Conf.Server.jwtpubkeyurl); err != nil {
			log.Panicf("Error while getting key %s: %v", Conf.Server.jwtpubkeyurl, err)
		}
	}
	if Conf.Server.jwtpubkeypath != "" {
		if err := auth.getjwtkey(Conf.Server.jwtpubkeypath); err != nil {
			log.Panicf("Error while getting key %s: %v", Conf.Server.jwtpubkeypath, err)
		}
	}
	proxy := NewProxy(Conf.S3, auth, messenger, sdaDB, tlsProxy)

	log.Debug("got the proxy ", proxy)

	http.Handle("/", proxy)

	hc := NewHealthCheck(8001, Conf.S3, Conf.Broker, tlsProxy)
	go hc.RunHealthChecks()

	server := &http.Server{
		Addr:              ":8000",
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 30 * time.Second,
	}

	if Conf.Server.cert != "" && Conf.Server.key != "" {
		if err := server.ListenAndServeTLS(Conf.Server.cert, Conf.Server.key); err != nil {
			panic(err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil {
			panic(err)
		}
	}
}
