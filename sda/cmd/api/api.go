// The api service exposes an api through a set of http(s) endpoints to interface towards the sensitive-data-archive
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/lestrrat-go/jwx/v2/jwk"
	apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
)

type dataset struct {
	AccessionIDs []string `json:"accession_ids"`
	DatasetID    string   `json:"dataset_id"`
	User         string   `json:"user"`
}

type API struct {
	auth        *userauth.ValidateFromToken
	enforcer    *casbin.Enforcer
	server      *http.Server
	inboxReader storage.Reader
	inboxWriter storage.Writer
	db          database.Database
	mq          broker.Broker
}

func main() {
	if err := run(); err != nil {
		slog.Error("api service failed", "err", err)
		os.Exit(1)
	}
}

func run() error {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := config.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	db, err := postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer db.Close()
	if dbSchemaVersion, err := db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	mq, err := rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer mq.Close()

	lb, err := locationbroker.NewLocationBroker(db)
	if err != nil {
		return fmt.Errorf("failed to initialize new location broker, due to: %v", err)
	}
	inboxWriter, err := storage.NewWriter(ctx, "inbox", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize inbox writer, due to: %v", err)
	}
	inboxReader, err := storage.NewReader(ctx, "inbox")
	if err != nil {
		return fmt.Errorf("failed to initialize inbox reader, reason: %v", err)
	}

	jwtPubKeyURL := apiconfig.JwtPubKeyURL()
	jwtPubKeyPath := apiconfig.JwtPubKeyPath()
	auth := userauth.NewValidateFromToken(jwk.NewSet())

	slog.Info("jwt", "public_key_url", jwtPubKeyURL)
	if jwtPubKeyURL != "" {
		if err := auth.FetchJwtPubKeyURL(jwtPubKeyURL); err != nil {
			return fmt.Errorf("failed to read JWT public key URL, reason: %v", err)
		}
	}

	slog.Info("jwt", "public_key_path", jwtPubKeyPath)
	if jwtPubKeyPath != "" {
		if err := auth.ReadJwtPubKeyPath(jwtPubKeyPath); err != nil {
			return fmt.Errorf("failed to read JWT public key path, reason: %v", err)
		}
	}

	m, err := model.NewModelFromString(jsonadapter.Model)
	if err != nil {
		return fmt.Errorf("failed to create json adapter model")
	}

	rbacFile, err := os.ReadFile(apiconfig.RbacFile())
	if err != nil {
		return fmt.Errorf("faield to read RBAC file, reason: %v", err)
	}

	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&rbacFile))
	if err != nil {
		return fmt.Errorf("failed to create new casbin enforcer instance, reason: %v", err)
	}

	app := API{auth: auth, enforcer: e, inboxReader: inboxReader, inboxWriter: inboxWriter, db: db, mq: mq}

	serverErr := make(chan error, 1)
	addr := apiconfig.ApiAddr()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	app.server = &http.Server{
		Addr:              addr,
		Handler:           app.routes(),
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      2 * time.Minute,
	}

	go func() {
		if apiconfig.ServerCert() != "" && apiconfig.ServerKey() != "" {
			slog.Info("starting", "addr", addr)
			if err := app.server.ListenAndServeTLS(apiconfig.ServerCert(), apiconfig.ServerKey()); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start https server, due to: %v", err)
			}
		} else {
			slog.Info("starting", "addr", addr)
			if err := app.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start http server, due to: %v", err)
			}
		}
	}()
	defer func() {
		serverShutdownCtx, serverShutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := app.server.Shutdown(serverShutdownCtx); err != nil {
			slog.Error("failed to close http/https server", "err", err)
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
