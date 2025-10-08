package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/config"
	internalConfig "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/config"
	validatorAPI "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/openapi/go-gin-server/go"
	log "github.com/sirupsen/logrus"
)

func main() {

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
	ginRouter := validatorAPI.NewRouter(validatorAPI.ApiHandleFunctions{ValidatorAPI: validatorAPIImpl})
	gin.SetMode(gin.ReleaseMode)

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

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Error starting server, due to: %v", err)
	}
}
