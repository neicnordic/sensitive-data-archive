package api

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/api/sda"
	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// healthResponse
func healthResponse(w http.ResponseWriter, r *http.Request) {
	// ok response to health
	w.WriteHeader(http.StatusOK)
}

// Setup configures the web server and registers the routes
func Setup() *http.Server {
	// Set up routing
	log.Info("(2/5) Registering endpoint handlers")
	r := mux.NewRouter().SkipClean(true)

	r.Handle("/metadata/datasets", middleware.TokenMiddleware(http.HandlerFunc(sda.Datasets))).Methods("GET")
	r.Handle("/metadata/datasets/{dataset:[^\\s/$.?#].[^\\s]+|[A-Za-z0-9-_:.]+}/files", middleware.TokenMiddleware(http.HandlerFunc(sda.Files))).Methods("GET")
	r.Handle("/files/{fileid:[A-Za-z0-9-_:.]+}", middleware.TokenMiddleware(http.HandlerFunc(sda.Download))).Methods("GET")
	r.HandleFunc("/health", healthResponse).Methods("GET")

	// Configure TLS settings
	log.Info("(3/5) Configuring TLS")
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	// Configure web server
	log.Info("(4/5) Configuring server")
	srv := &http.Server{
		Addr:              config.Config.App.Host + ":" + fmt.Sprint(config.Config.App.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      20 * time.Second,
	}

	return srv
}
