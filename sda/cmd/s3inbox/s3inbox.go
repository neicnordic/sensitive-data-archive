package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gorilla/mux"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"

	log "github.com/sirupsen/logrus"
)

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
		log.Fatalf("failed to config due to: %v", err)
	}

	tlsProxy, err := config.TLSConfigProxy(conf)
	if err != nil {
		log.Fatalf("failed to setup tls config due to: %v", err)
	}

	sdaDB, err := database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatalf("failed to init new db due to: %v", err)
	}
	if sdaDB.Version < 23 {
		log.Fatalf("database schema v23 is required")
	}

	log.Debugf("Connected to sda-db (v%v)", sdaDB.Version)
	log.Println(conf.S3Inbox.Endpoint + "/BUCKET == " + conf.S3Inbox.Bucket)
	s3, err := newS3Client(conf.S3Inbox)
	if err != nil {
		log.Fatalf("failed to init new S3 client due to: %v", err)
	}

	err = checkS3Bucket(conf.S3Inbox.Bucket, s3)
	if err != nil {
		log.Fatalf("failed to check if inbox bucket exists due to: %v", err)
	}

	messenger, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatalf("failed to init broker due to: %v", err)
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
	if conf.Server.Jwtpubkeyurl != "" {
		if err := auth.FetchJwtPubKeyURL(conf.Server.Jwtpubkeyurl); err != nil {
			log.Panicf("Error while getting key %s: %v", conf.Server.Jwtpubkeyurl, err)
		}
	}
	if conf.Server.Jwtpubkeypath != "" {
		if err := auth.ReadJwtPubKeyPath(conf.Server.Jwtpubkeypath); err != nil {
			log.Panicf("Error while getting key %s: %v", conf.Server.Jwtpubkeypath, err)
		}
	}
	router := mux.NewRouter()
	proxy := NewProxy(conf.S3Inbox, auth, messenger, sdaDB, tlsProxy)
	router.HandleFunc("/", proxy.CheckHealth).Methods("HEAD")
	router.HandleFunc("/health", proxy.CheckHealth)
	router.PathPrefix("/").Handler(proxy)

	server := &http.Server{
		Addr:              ":8000",
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 30 * time.Second,
		Handler:           router,
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

func checkS3Bucket(bucket string, s3Client *s3.Client) error {
	_, err := s3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{Bucket: &bucket})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			var bae *types.BucketAlreadyExists
			var baoby *types.BucketAlreadyOwnedByYou
			if errors.As(err, &bae) || errors.As(err, &baoby) {
				return nil
			}

			return fmt.Errorf("unexpected issue while creating bucket: %s", err.Error())
		}

		return fmt.Errorf("verifying bucket failed, check S3 configuration: %s", err.Error())
	}

	return nil
}
