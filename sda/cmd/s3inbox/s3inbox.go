package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gorilla/mux"
	"github.com/lestrrat-go/jwx/v2/jwk"
	s3inboxconf "github.com/neicnordic/sensitive-data-archive/cmd/s3inbox/config"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
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

	if err := configv2.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	s3InboxConf := s3InboxConfig{
		endpoint:  s3inboxconf.S3InboxEndpoint(),
		accessKey: s3inboxconf.S3InboxAccessKey(),
		secretKey: s3inboxconf.S3InboxSecretKey(),
		bucket:    s3inboxconf.S3InboxBucket(),
		region:    s3inboxconf.S3InboxRegion(),
		caCert:    s3inboxconf.S3InboxCaCert(),
		readyPath: s3inboxconf.S3InboxReadyPath(),
	}

	tlsProxy, err := configTLS(s3InboxConf)
	if err != nil {
		return fmt.Errorf("failed to setup tls config due to: %v", err)
	}

	db, err := postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db due to: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()
	if dbSchemaVersion, err := db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	s3Client, err := newS3Client(ctx, s3InboxConf)
	if err != nil {
		return fmt.Errorf("failed to initialize new S3 client due to: %v", err)
	}

	if err = checkS3Bucket(ctx, s3Client, s3inboxconf.S3InboxBucket()); err != nil {
		return fmt.Errorf("failed to check if inbox bucket exists due to: %v", err)
	}

	mqBroker, err := rabbitmq.NewRabbitMQBroker(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize broker due to: %v", err)
	}
	defer func() {
		if mqBroker == nil {
			return
		}

		if err := mqBroker.Close(); err != nil {
			slog.Error("failed to close broker", "error", err)
		}
	}()

	auth := userauth.NewValidateFromToken(jwk.NewSet())

	// Load keys for JWT verification
	jwtPubKeyURL := s3inboxconf.ServerJwtPubKeyURL()
	jwtPubKeyPath := s3inboxconf.ServerJwtPubKeyPath()
	if jwtPubKeyURL == "" && jwtPubKeyPath == "" {
		return errors.New("no JWT public key url or JWT public key path specified")
	}
	if jwtPubKeyURL != "" {
		if err := auth.FetchJwtPubKeyURL(jwtPubKeyURL); err != nil {
			return fmt.Errorf("failed to read jwt pub key from url: %s, due to %v", jwtPubKeyURL, err)
		}
	}
	if jwtPubKeyPath != "" {
		if err := auth.ReadJwtPubKeyPath(jwtPubKeyPath); err != nil {
			return fmt.Errorf("failed to read jwt pub key from path: %s, due to %v", jwtPubKeyPath, err)
		}
	}

	router := mux.NewRouter()
	proxy := newProxy(s3InboxConf, s3Client, auth, mqBroker, db, tlsProxy, s3inboxconf.DestinationQueue())
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
		serverCert := s3inboxconf.ServerCert()
		serverKey := s3inboxconf.ServerKey()
		if serverCert != "" && serverKey != "" {
			slog.Info("starting https server")
			if err := server.ListenAndServeTLS(serverCert, serverKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start https server, due to: %v", err)
			}
		} else {
			slog.Info("starting http server")
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

func checkS3Bucket(ctx context.Context, s3Client *s3.Client, bucket string) error {
	_, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket})
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

		return fmt.Errorf("verifying bucket failed, check S3 configuration: %v", err)
	}

	return nil
}

func configTLS(c s3InboxConfig) (*tls.Config, error) {
	cfg := new(tls.Config)

	// Read system CAs
	systemCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("failed to read system CAs: %v, using an empty pool as base", err)
		systemCAs = x509.NewCertPool()
	}

	cfg.RootCAs = systemCAs

	if c.caCert != "" {
		caCert, e := os.ReadFile(c.caCert) // #nosec G703 -- file path controlled by configuration
		if e != nil {
			return nil, fmt.Errorf("failed to append %q to RootCAs: %v", c.caCert, e)
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(caCert); !ok {
			log.Debug("no certs appended, using system certs only")
		}
	}

	return cfg, nil
}

func newS3Client(ctx context.Context, conf s3InboxConfig) (*s3.Client, error) {
	tlsConfig, err := configTLS(conf)
	if err != nil {
		return nil, err
	}

	s3cfg, err := s3config.LoadDefaultConfig(
		ctx,
		s3config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(conf.accessKey, conf.secretKey, "")),
		s3config.WithHTTPClient(&http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}}),
	)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(conf.endpoint)
			o.EndpointOptions.DisableHTTPS = strings.HasPrefix(conf.endpoint, "http:")
			o.Region = conf.region
			o.UsePathStyle = true
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		},
	)

	return s3Client, nil
}
