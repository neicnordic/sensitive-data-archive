// Package main is the entry point for the download service.
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
	log "github.com/sirupsen/logrus"

	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/handlers"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/health"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	internalconfig "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
)

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	gin.SetMode(gin.ReleaseMode)

	if err := run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}

func run() error {
	// Setup context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Load configuration
	// Note: config package init() registers flags with internalconfig
	_ = config.APIPort() // Ensure config package is imported and init() runs
	if err := internalconfig.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Start gRPC health server
	go func() {
		if err := health.Start(config.HealthPort()); err != nil {
			log.Errorf("health server error: %v", err)
			cancel()
		}
	}()
	defer health.Stop()

	// Initialize database
	if err := database.Init(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		log.Info("closing database connection...")
		if err := database.Close(); err != nil {
			log.Errorf("database close error: %v", err)
		}
	}()

	// Initialize authentication middleware
	if err := middleware.InitAuth(); err != nil {
		return fmt.Errorf("failed to initialize auth: %w", err)
	}

	// TODO: Initialize storage/v2 reader
	// TODO: Initialize gRPC reencrypt client

	// Setup HTTP server
	router := gin.New()
	router.Use(gin.Recovery())

	// Add request logging
	if log.IsLevelEnabled(log.InfoLevel) {
		router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
			Formatter: func(params gin.LogFormatterParams) string {
				return fmt.Sprintf(`{"level":"info","method":"%s","path":"%s","status":%d,"latency":"%v","client_ip":"%s","time":"%s"}`+"\n",
					params.Method,
					params.Path,
					params.StatusCode,
					params.Latency,
					params.ClientIP,
					params.TimeStamp.Format(time.RFC3339),
				)
			},
			Output:    os.Stdout,
			SkipPaths: []string{"/health/live"},
		}))
	}

	// Create handlers with dependencies
	h, err := handlers.New(
		handlers.WithDatabase(database.GetDB()),
		handlers.WithGRPCReencryptHost(config.GRPCHost()),
		handlers.WithGRPCReencryptPort(config.GRPCPort()),
	)
	if err != nil {
		return fmt.Errorf("failed to create handlers: %w", err)
	}
	h.RegisterRoutes(router)

	// Configure TLS if certificates are provided
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", config.APIHost(), config.APIPort()),
		Handler:           router,
		TLSConfig:         tlsConfig,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      30 * time.Minute, // Long timeout for large file downloads
	}

	// Start server in goroutine
	go func() {
		var err error
		if config.APIServerCert() != "" && config.APIServerKey() != "" {
			log.Infof("server listening at: https://%s", srv.Addr)
			err = srv.ListenAndServeTLS(config.APIServerCert(), config.APIServerKey())
		} else {
			log.Infof("server listening at: http://%s", srv.Addr)
			err = srv.ListenAndServe()
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("server error: %v", err)
			cancel()
		}
	}()

	// Mark service as ready
	health.SetServingStatus(health.Serving)
	log.Infof("health server listening at: :%d", config.HealthPort())

	// Wait for shutdown signal
	select {
	case <-sigc:
		log.Info("received shutdown signal")
	case <-ctx.Done():
		log.Info("context cancelled")
	}

	// Mark service as not serving
	health.SetServingStatus(health.NotServing)

	// Graceful shutdown
	log.Info("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("server shutdown error: %v", err)
	}

	log.Info("shutdown complete")

	return nil
}
