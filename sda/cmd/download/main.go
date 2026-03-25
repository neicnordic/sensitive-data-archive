// Package main is the entry point for the download service.
package main

import (
	"context"
	"crypto/rand"
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

	"github.com/neicnordic/sensitive-data-archive/cmd/download/audit"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/config"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/database"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/handlers"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/health"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/middleware"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/cmd/download/visa"
	internalconfig "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	storage "github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
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

	// Validate permission model
	if err := validatePermissionModel(config.PermissionModel(), config.VisaEnabled()); err != nil {
		return err
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

	// Wrap database with cache if enabled
	if config.CacheEnabled() {
		cachedDB, err := database.NewCachedDB(database.GetDB(), database.CacheConfig{
			FileTTL:       time.Duration(config.CacheFileTTL()) * time.Second,
			PermissionTTL: time.Duration(config.CachePermissionTTL()) * time.Second,
			DatasetTTL:    time.Duration(config.CacheDatasetTTL()) * time.Second,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize database cache: %w", err)
		}
		database.RegisterDatabase(cachedDB)
		log.Info("Database caching enabled")
	}

	// Production safety guards
	if config.Environment() == "production" {
		if err := validateProductionConfig(productionConfig{
			AllowAllData:   config.JWTAllowAllData(),
			HMACSecret:     config.PaginationHMACSecret(),
			GRPCClientCert: config.GRPCClientCert(),
			GRPCClientKey:  config.GRPCClientKey(),
		}); err != nil {
			return err
		}
	}

	// Initialize audit logger
	var auditLogger audit.Logger
	if config.AuditRequired() {
		auditLogger = audit.NewStdoutLogger()
	} else {
		auditLogger = audit.NoopLogger{}
	}

	// Initialize pagination HMAC secret
	if secret := config.PaginationHMACSecret(); secret != "" {
		handlers.SetPaginationSecret([]byte(secret))
	} else {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("failed to generate pagination secret: %w", err)
		}

		handlers.SetPaginationSecret(b)
		log.Warn("pagination.hmac-secret not configured: auto-generated random key. " +
			"Page tokens will not survive restarts or work across replicas. " +
			"Set pagination.hmac-secret for multi-replica deployments.")
	}

	// Initialize authentication middleware
	if err := middleware.InitAuth(); err != nil {
		return fmt.Errorf("failed to initialize auth: %w", err)
	}

	// Initialize GA4GH visa validator if enabled
	var visaValidator *visa.Validator
	if config.VisaEnabled() {
		vv, err := initVisaValidator()
		if err != nil {
			return fmt.Errorf("failed to initialize visa validator: %w", err)
		}
		visaValidator = vv
		log.Infof("GA4GH visa support enabled (source=%s, identity=%s, permission=%s)",
			config.VisaSource(), config.VisaIdentityMode(), config.PermissionModel())
	}

	// Initialize storage/v2 reader
	storageReader, err := storage.NewReader(ctx, config.StorageBackend())
	if err != nil {
		return fmt.Errorf("failed to initialize storage reader: %w", err)
	}
	log.Infof("storage reader initialized for backend: %s", config.StorageBackend())

	// Initialize gRPC reencrypt client
	reencryptOpts := []reencrypt.ClientOption{
		reencrypt.WithTimeout(time.Duration(config.GRPCTimeout()) * time.Second),
	}
	if config.GRPCClientCert() != "" && config.GRPCClientKey() != "" {
		reencryptOpts = append(reencryptOpts, reencrypt.WithTLS(
			config.GRPCCACert(),
			config.GRPCClientCert(),
			config.GRPCClientKey(),
		))
	}
	reencryptClient := reencrypt.NewClient(config.GRPCHost(), config.GRPCPort(), reencryptOpts...)
	defer func() {
		if err := reencryptClient.Close(); err != nil {
			log.Errorf("failed to close reencrypt client: %v", err)
		}
	}()
	log.Infof("reencrypt client configured for %s:%d", config.GRPCHost(), config.GRPCPort())

	// Setup HTTP server
	router := gin.New()
	router.UseRawPath = true         // Route on raw URL-encoded path (supports %2F in dataset IDs)
	router.UnescapePathValues = true // c.Param() still returns decoded value
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
	handlerOpts := []func(*handlers.Handlers){
		handlers.WithDatabase(database.GetDB()),
		handlers.WithStorageReader(storageReader),
		handlers.WithReencryptClient(reencryptClient),
		handlers.WithAuditLogger(auditLogger),
	}
	if visaValidator != nil {
		handlerOpts = append(handlerOpts, handlers.WithVisaValidator(visaValidator))
	}

	h, err := handlers.New(handlerOpts...)
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

// initVisaValidator creates and configures the GA4GH visa validator.
func initVisaValidator() (*visa.Validator, error) {
	// Load trusted issuers
	trustedPath := config.VisaTrustedIssuersPath()
	if trustedPath == "" {
		return nil, errors.New("visa.trusted-issuers-path is required when visa is enabled")
	}

	allowInsecure := config.VisaAllowInsecureJKU()
	if allowInsecure {
		log.Warn("visa.allow-insecure-jku is enabled: HTTP JKU URLs are permitted (NOT for production use)")
	}

	trustedIssuers, err := visa.LoadTrustedIssuers(trustedPath, allowInsecure)
	if err != nil {
		return nil, fmt.Errorf("failed to load trusted issuers: %w", err)
	}
	log.Infof("loaded %d trusted issuer+JKU pairs from %s", len(trustedIssuers), trustedPath)

	// Discover userinfo URL if not explicitly configured
	userinfoURL := config.VisaUserinfoURL()
	if userinfoURL == "" && config.OIDCIssuer() != "" {
		discovered, err := visa.DiscoverUserinfoURL(config.OIDCIssuer())
		if err != nil {
			log.Warnf("OIDC discovery failed: %v (userinfo will need to be configured explicitly)", err)
		} else {
			userinfoURL = discovered
			log.Infof("discovered userinfo endpoint: %s", userinfoURL)
		}
	}

	cfg := visa.ValidatorConfig{
		Source:             config.VisaSource(),
		UserinfoURL:        userinfoURL,
		DatasetIDMode:      config.VisaDatasetIDMode(),
		IdentityMode:       config.VisaIdentityMode(),
		ValidateAsserted:   config.VisaValidateAsserted(),
		ClockSkew:          30 * time.Second,
		MaxVisas:           config.VisaMaxVisas(),
		MaxJWKSPerReq:      config.VisaMaxJWKSPerRequest(),
		MaxVisaSize:        config.VisaMaxVisaSize(),
		JWKCacheTTL:        time.Duration(config.VisaCacheJWKTTL()) * time.Second,
		ValidationCacheTTL: time.Duration(config.VisaCacheValidationTTL()) * time.Second,
		UserinfoCacheTTL:   time.Duration(config.VisaCacheUserinfoTTL()) * time.Second,
	}

	return visa.NewValidator(cfg, trustedIssuers, database.GetDB())
}

// productionConfig holds the values checked by production safety guards.
type productionConfig struct {
	AllowAllData   bool
	HMACSecret     string
	GRPCClientCert string
	GRPCClientKey  string
}

// validatePermissionModel checks that the permission model is valid and
// that its dependencies are satisfied.
func validatePermissionModel(model string, visaEnabled bool) error {
	switch model {
	case "ownership", "combined":
		return nil
	case "visa":
		if !visaEnabled {
			return fmt.Errorf("permission.model is %q but visa.enabled is false — this combination would deny all requests", model)
		}

		return nil
	default:
		return fmt.Errorf("invalid permission.model %q: must be ownership, visa, or combined", model)
	}
}

// validateProductionConfig checks that dangerous testing flags are disabled
// and required security configuration is present for production deployments.
func validateProductionConfig(cfg productionConfig) error {
	if cfg.AllowAllData {
		return errors.New("jwt.allow-all-data must not be enabled in production")
	}

	if cfg.HMACSecret == "" {
		return errors.New("pagination.hmac-secret is required in production (tokens must be portable across replicas)")
	}

	if cfg.GRPCClientCert == "" || cfg.GRPCClientKey == "" {
		return errors.New("grpc.client-cert and grpc.client-key are required in production")
	}

	return nil
}
