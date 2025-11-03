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
	validatorapi "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api/openapi_interface"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/config"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/commandexecutor"
	internalconfig "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/ginmiddleware/authenticator"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/ginmiddleware/rbac"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/jobpreparationworker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/jobworker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	log "github.com/sirupsen/logrus"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	if err := internalconfig.Load(); err != nil {
		log.Fatalf("failed to load config due to: %v", err)
	}

	if err := validators.Init(config.ValidatorPaths()); err != nil {
		log.Fatalf("failed to initialize validators due to: %v", err)
	}

	amqpBroker, err := broker.NewAMQPBroker()
	if err != nil {
		log.Fatalf("failed to create new AMPQ broker due to: %v", err)
	}

	if err := postgres.Init(); err != nil {
		log.Fatalf("failed to initialise postgres database due to: %v", err)
	}

	jobpreparationworkers, err := jobpreparationworker.NewWorkers(
		jobpreparationworker.WorkerCount(config.JobPreparationWorkerCount()),
		jobpreparationworker.Broker(amqpBroker),
		jobpreparationworker.SourceQueue(config.JobPreparationQueue()),
		jobpreparationworker.SdaAPIToken(config.SdaAPIToken()),
		jobpreparationworker.SdaAPIURL(config.SdaAPIURL()),
		jobpreparationworker.DestinationQueue(config.JobQueue()),
		jobpreparationworker.ValidationWorkDirectory(config.ValidationWorkDir()),
	)
	if err != nil {
		log.Fatalf("failed to initialize job preparation workers due to: %v", err)
	}

	jobworkers, err := jobworker.NewWorkers(
		jobworker.WorkerCount(config.JobWorkerCount()),
		jobworker.Broker(amqpBroker),
		jobworker.SourceQueue(config.JobQueue()),
		jobworker.CommandExecutor(&commandexecutor.OsCommandExecutor{}),
	)
	if err != nil {
		log.Fatalf("failed to initialize job preparation workers due to: %v", err)
	}

	validatorAPIImpl, err := api.NewValidatorAPIImpl(
		api.SdaAPIURL(config.SdaAPIURL()),
		api.SdaAPIToken(config.SdaAPIToken()),
		api.Broker(amqpBroker),
		api.ValidationJobPreparationQueue(config.JobPreparationQueue()),
		api.ValidationFileSizeLimit(config.ValidationFileSizeLimit()),
	)
	if err != nil {
		log.Fatalf("failed to create new validator API impl, due to: %v", err)
	}

	ginRouter := gin.Default()

	authMiddleware, err := authenticator.NewAuthenticator()
	if err != nil {
		log.Fatalf("failed to create authenticator middleware, due to: %v", err)
	}
	rbacMiddleware, err := rbac.NewRbac()
	if err != nil {
		log.Fatalf("failed to create rbac middleware, due to: %v", err)
	}
	ginRouter.Use(authMiddleware.Authenticate(), rbacMiddleware.Enforce())

	validatorapi.NewRouterWithGinEngine(ginRouter, validatorapi.ApiHandleFunctions{ValidatorOrchestratorAPI: validatorAPIImpl})

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.APIPort()),
		Handler:           ginRouter,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      -1,
	}

	go func() {
		if err := <-jobworkers.Monitor(); err != nil {
			log.Errorf("job workers failed: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	go func() {
		if err := <-jobpreparationworkers.Monitor(); err != nil {
			log.Errorf("job preparation workers failed: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	go func() {
		log.Infof("server listening at: %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Error starting server, due to: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	go func() {
		if err := <-amqpBroker.ConnectionWatcher(); err != nil {
			log.Errorf("broker connection error: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	<-sigc

	log.Info("gracefully shutting down")

	log.Infof("shutting down HTTP server")
	serverShutdownCtx, serverShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := srv.Shutdown(serverShutdownCtx); err != nil {
		log.Errorf("failed to gracefully shutdown the server due to: %v", err)
	}
	serverShutdownCancel()

	log.Infof("shutting down job preparation workers")
	jobpreparationworkers.Shutdown()

	log.Infof("shutting down job workers")
	jobworkers.Shutdown()

	log.Infof("shutting broker connection")
	if err := amqpBroker.Close(); err != nil {
		log.Errorf("failed to gracefully shutdown broker connection")
	}

	log.Infof("shutting database connection")
	if err := database.Close(); err != nil {
		log.Errorf("failed to gracefully database connection")
	}

	os.Exit(1)
}
