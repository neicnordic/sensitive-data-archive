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
	validatorAPI "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api/openapi_interface"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/config"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/command_executor"
	internalConfig "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/gin_middleware/authenticator"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/gin_middleware/rbac"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/job_preparation_worker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/job_worker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	log "github.com/sirupsen/logrus"
)

func main() {
	gin.SetMode(gin.ReleaseMode)

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	if err := internalConfig.Load(); err != nil {
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

	if err := job_preparation_worker.Init(
		job_preparation_worker.WorkerCount(config.JobPreparationWorkerCount()),
		job_preparation_worker.Broker(amqpBroker),
		job_preparation_worker.SourceQueue(config.JobPreparationQueue()),
		job_preparation_worker.SdaApiToken(config.SdaApiToken()),
		job_preparation_worker.SdaApiUrl(config.SdaApiUrl()),
		job_preparation_worker.DestinationQueue(config.JobQueue()),
		job_preparation_worker.ValidationWorkDirectory(config.ValidationWorkDir()),
	); err != nil {
		log.Fatalf("failed to initialize job preparation workers due to: %v", err)
	}

	if err := job_worker.Init(
		job_worker.WorkerCount(config.JobWorkerCount()),
		job_worker.Broker(amqpBroker),
		job_worker.SourceQueue(config.JobQueue()),
		job_worker.CommandExecutor(&command_executor.OsCommandExecutor{}),
	); err != nil {
		log.Fatalf("failed to initialize job preparation workers due to: %v", err)
	}

	validatorAPIImpl, err := api.NewValidatorAPIImpl(
		api.SdaApiUrl(config.SdaApiUrl()),
		api.SdaApiToken(config.SdaApiToken()),
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

	validatorAPI.NewRouterWithGinEngine(ginRouter, validatorAPI.ApiHandleFunctions{ValidatorOrchestratorAPI: validatorAPIImpl})

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
		if err := job_worker.StartWorkers(); err != nil {
			log.Errorf("job workers failed: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	go func() {
		if err := job_preparation_worker.StartWorkers(); err != nil {
			log.Errorf("job preparation workers failed: %v", err)
			sigc <- syscall.SIGTERM
		}
	}()

	go func() {
		log.Infof("server listening at: %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(http.ErrServerClosed, err) {
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
	defer serverShutdownCancel()
	if err := srv.Shutdown(serverShutdownCtx); err != nil {
		log.Errorf("failed to gracefully shutdown the server due to: %v", err)
	}

	log.Infof("shutting down job preparation workers")
	job_preparation_worker.ShutdownWorkers()

	log.Infof("shutting down job workers")
	job_worker.ShutdownWorkers()

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
