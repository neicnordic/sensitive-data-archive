package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/api/s3"
	"github.com/neicnordic/sda-download/api/sda"
	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// SelectedMiddleware returns the middleware chain based on configuration.
// For example, config.Config.App.Middleware could be "default", "token", etc.
var SelectedMiddleware = func() []gin.HandlerFunc {
	switch strings.ToLower(config.Config.App.Middleware) {
	case "default":
		return middleware.ChainDefaultMiddleware()
	case "token":
		return []gin.HandlerFunc{middleware.TokenMiddleware()}
	default:
		return nil
	}
}

// healthResponse
func healthResponse(c *gin.Context) {
	// ok response to health
	c.Writer.WriteHeader(http.StatusOK)
}

// Setup configures the web server and registers the routes
func Setup() *http.Server {
	// Set up routing
	log.Info("(2/5) Registering endpoint handlers")
	if log.GetLevel() != log.DebugLevel {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if log.GetLevel() == log.DebugLevel {
		router.Use(gin.LoggerWithConfig(
			gin.LoggerConfig{
				Formatter: func(params gin.LogFormatterParams) string {
					s, _ := json.Marshal(map[string]any{
						"level":       "debug",
						"method":      params.Method,
						"path":        params.Path,
						"remote_addr": params.ClientIP,
						"status_code": params.StatusCode,
						"time":        params.TimeStamp.Format(time.RFC3339),
					})

					return string(s) + "\n"
				},

				Skip: func(c *gin.Context) bool {
					// skip logging HEAD requests to / and all requests to /health
					// (HEAD request to health are redirected to path "")
					return (c.Request.Method == "HEAD" && strings.Trim(c.FullPath(), "/") == "") ||
						c.FullPath() == "/health"
				},
				Output: gin.DefaultWriter,
			},
		))
	}

	router.HandleMethodNotAllowed = true
	mw := SelectedMiddleware()

	// protected endpoints
	router.GET("/metadata/datasets", append(mw, sda.Datasets)...)
	router.GET("/metadata/datasets/*dataset", append(mw, sda.Files)...)
	router.GET("/files/:fileid", append(mw, sda.Download)...)
	router.GET("/s3/*path", append(mw, s3.Download)...)
	router.HEAD("/s3/*path", append(mw, s3.Download)...)

	// public endpoints
	router.GET("/health", healthResponse)
	router.HEAD("/", healthResponse)

	// Configure TLS settings
	log.Info("(3/5) Configuring TLS")
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	// Configure web server
	log.Info("(4/5) Configuring server")
	srv := &http.Server{
		Addr:              config.Config.App.Host + ":" + fmt.Sprint(config.Config.App.Port),
		Handler:           router,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      -1,
	}

	return srv
}
