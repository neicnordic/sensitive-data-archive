package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/config"
	internalConfig "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	validatorAPI "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/openapi/go-gin-server/go"
	log "github.com/sirupsen/logrus"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	if err := internalConfig.Load(); err != nil {
		log.Fatal(err)
	}

	validatorAPIImpl, err := api.NewValidatorAPIImpl(
		api.ValidatorPaths(config.ValidatorPaths()),
		api.SdaApiUrl(config.SdaApiUrl()),
		api.SdaApiToken(config.SdaApiToken()),
		api.ValidationWorkDir(config.ValidationWorkDir()),
	)
	if err != nil {
		log.Fatalf("failed to create new validator API impl, err: %v", err)
	}
	ginRouter := validatorAPI.NewRouter(validatorAPI.ApiHandleFunctions{ValidatorOrchestratorAPI: validatorAPIImpl})

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.ApiPort()),
		Handler:           ginRouter,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      -1,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(http.ErrServerClosed, err) {
			log.Fatalf("Error starting server, due to: %v", err)
		}
	}()

	log.Infof("server listening at: %s", srv.Addr)
	<-sigc

	log.Info("gracefully shutting down server")
	serverShutdownCtx, serverShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer serverShutdownCancel()
	if err := srv.Shutdown(serverShutdownCtx); err != nil {
		log.Fatal("failed to gracefully shutdown the server")
	}
}
