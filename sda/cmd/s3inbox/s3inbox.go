package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	conf, err := config.NewConfig("s3inbox")
	if err != nil {
		return fmt.Errorf("failed to load config due to: %v", err)
	}

	tlsProxy, err := configTLS(conf.S3Inbox)
	if err != nil {
		return fmt.Errorf("failed to setup tls config due to: %v", err)
	}

	sdaDB, err := database.NewSDAdb(conf.Database)
	if err != nil {
		return fmt.Errorf("failed to initialize sda db due to: %v", err)
	}
	defer sdaDB.Close()
	if sdaDB.Version < 23 {
		return errors.New("database schema v23 is required")
	}

	log.Debugf("Connected to sda-db (v%v)", sdaDB.Version)
	log.Println(conf.S3Inbox.Endpoint + "/BUCKET == " + conf.S3Inbox.Bucket)
	s3Client, err := newS3Client(conf.S3Inbox)
	if err != nil {
		return fmt.Errorf("failed to initialize new S3 client due to: %v", err)
	}

	if err = checkS3Bucket(conf.S3Inbox.Bucket, s3Client); err != nil {
		return fmt.Errorf("failed to check if inbox bucket exists due to: %v", err)
	}

	mqBroker, err := broker.NewMQ(conf.Broker)
	if err != nil {
		return fmt.Errorf("failed to initialize broker due to: %v", err)
	}
	defer func() {
		if err := mqBroker.Channel.Close(); err != nil {
			log.Errorf("failed to close mq broker channel due to: %v", err)
		}
		if err := mqBroker.Connection.Close(); err != nil {
			log.Errorf("failed to close mq broker connection due to: %v", err)
		}
	}()

	auth := userauth.NewValidateFromToken(jwk.NewSet())
	// Load keys for JWT verification
	if conf.Server.Jwtpubkeyurl != "" {
		if err := auth.FetchJwtPubKeyURL(conf.Server.Jwtpubkeyurl); err != nil {
			return fmt.Errorf("failed to read jwt pub key from url: %s, due to %v", conf.Server.Jwtpubkeyurl, err)
		}
	}
	if conf.Server.Jwtpubkeypath != "" {
		if err := auth.ReadJwtPubKeyPath(conf.Server.Jwtpubkeypath); err != nil {
			return fmt.Errorf("failed to read jwt pub key from path: %s, due to %v", conf.Server.Jwtpubkeypath, err)
		}
	}
	router := mux.NewRouter()
	proxy := NewProxy(conf.S3Inbox, auth, mqBroker, sdaDB, tlsProxy)
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

	serverErr := make(chan error, 1)
	go func() {
		if conf.Server.Cert != "" && conf.Server.Key != "" {
			if err := server.ListenAndServeTLS(conf.Server.Cert, conf.Server.Key); err != nil {
				serverErr <- fmt.Errorf("failed to start https server, due to: %v", err)
			}
		} else {
			if err := server.ListenAndServe(); err != nil {
				serverErr <- fmt.Errorf("failed to start http server, due to: %v", err)
			}
		}
	}()

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
	case err := <-serverErr:
		return err
	}

	if err := server.Shutdown(context.Background()); err != nil {
		log.Errorf("failed to close http/https server due to: %v", err)
	}

	return nil
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

func configTLS(c config.S3InboxConf) (*tls.Config, error) {
	cfg := new(tls.Config)

	log.Debug("setting up TLS for S3 connection")

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v", err)

		return nil, err
	}

	cfg.RootCAs = systemCAs

	if c.CAcert != "" {
		cacert, e := os.ReadFile(c.CAcert) // #nosec this file comes from our configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", cacert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(cacert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}
