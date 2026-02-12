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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	s3Client, err := newS3Client(ctx, conf.S3Inbox)
	if err != nil {
		return fmt.Errorf("failed to initialize new S3 client due to: %v", err)
	}

	if err = checkS3Bucket(ctx, conf.S3Inbox.Bucket, s3Client); err != nil {
		return fmt.Errorf("failed to check if inbox bucket exists due to: %v", err)
	}

	mqBroker, err := broker.NewMQ(conf.Broker)
	if err != nil {
		return fmt.Errorf("failed to initialize broker due to: %v", err)
	}
	defer func() {
		if mqBroker == nil {
			return
		}
		if mqBroker.Channel != nil {
			if err := mqBroker.Channel.Close(); err != nil {
				log.Errorf("failed to close mq broker channel due to: %v", err)
			}
		}
		if mqBroker.Connection != nil {
			if err := mqBroker.Connection.Close(); err != nil {
				log.Errorf("failed to close mq broker connection due to: %v", err)
			}
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
			if err := server.ListenAndServeTLS(conf.Server.Cert, conf.Server.Key); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start https server, due to: %v", err)
			}
		} else {
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start http server, due to: %v", err)
			}
		}
	}()
	defer func() {
		serverShutdownCtx, serverShutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := server.Shutdown(serverShutdownCtx); err != nil {
			log.Errorf("failed to close http/https server due to: %v", err)
		}
		serverShutdownCancel()
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
		return nil
	case err := <-serverErr:
		return err
	}
}

func checkS3Bucket(ctx context.Context, bucket string, s3Client *s3.Client) error {
	_, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			if apiErr.ErrorCode() == "NotFound" {
				return fmt.Errorf("bucket: %s does not exist at the configured s3 endpoint", bucket)
			}

			return fmt.Errorf("unexpected issue while checking that bucket: %s exists, due to %v", bucket, err)
		}

		return fmt.Errorf("verifying bucket failed, check S3 configuration: %v", err)
	}

	return nil
}

func configTLS(c config.S3InboxConf) (*tls.Config, error) {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}

	cfg.RootCAs = systemCAs

	if c.CaCert != "" {
		caCert, e := os.ReadFile(c.CaCert)
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", c.CaCert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(caCert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}
