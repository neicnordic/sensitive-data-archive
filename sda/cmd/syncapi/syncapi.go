package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"

	log "github.com/sirupsen/logrus"
)

var Conf *config.Config
var err error

func main() {
	Conf, err = config.NewConfig("sync-api")
	if err != nil {
		log.Fatal(err)
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		log.Fatal(err)
	}

	sigc := make(chan os.Signal, 5)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sigc
		shutdown()
		os.Exit(0)
	}()

	srv := setup(Conf)

	if Conf.API.ServerCert != "" && Conf.API.ServerKey != "" {
		log.Infof("Web server is ready to receive connections at https://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServeTLS(Conf.API.ServerCert, Conf.API.ServerKey); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	} else {
		log.Infof("Web server is ready to receive connections at http://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServe(); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	}
}

func setup(config *config.Config) *http.Server {
	r := mux.NewRouter().SkipClean(true)

	r.HandleFunc("/ready", readinessResponse).Methods("GET")
	r.HandleFunc("/accession", basicAuth(http.HandlerFunc(accession))).Methods("POST")
	r.HandleFunc("/dataset", basicAuth(http.HandlerFunc(dataset))).Methods("POST")
	r.HandleFunc("/ingest", basicAuth(http.HandlerFunc(ingest))).Methods("POST")
	r.HandleFunc("/metadata", basicAuth(http.HandlerFunc(metadata))).Methods("POST")

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              config.API.Host + ":" + fmt.Sprint(config.API.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      -1,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 20 * time.Second,
	}

	return srv
}

func shutdown() {
	defer Conf.API.MQ.Channel.Close()
	defer Conf.API.MQ.Connection.Close()
}

func readinessResponse(w http.ResponseWriter, _ *http.Request) {
	statusCocde := http.StatusOK

	if Conf.API.MQ.Connection.IsClosed() {
		statusCocde = http.StatusServiceUnavailable
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	if Conf.API.MQ.Channel.IsClosed() {
		statusCocde = http.StatusServiceUnavailable
		Conf.API.MQ.Connection.Close()
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	w.WriteHeader(statusCocde)
}

func dataset(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to read request body")

		return
	}
	defer r.Body.Close()

	var d struct {
		Type         string   `json:"type"`
		DatasetID    string   `json:"dataset_id"`
		AccessionIDs []string `json:"accession_ids,omitempty"`
	}
	_ = json.Unmarshal(b, &d)

	var action string
	switch d.Type {
	case "mapping":
		action = "mapping"
	case "release":
		action = "release"
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-%s.json", Conf.Broker.SchemasPath, action), b); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("eror on JSON validation: %s", err.Error()))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	log.Infoln(payload)
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, err = w.Write(response)
	if err != nil {
		log.Errorf("failed to write HTTP response, reason: %v", err)
	}
}

func metadata(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to read request body")

		return
	}
	defer r.Body.Close()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/bigpicture/metadata-sync.json", Conf.Broker.SchemasPath), b); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func basicAuth(auth http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok {
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			expectedUsernameHash := sha256.Sum256([]byte(Conf.SyncAPI.APIUser))
			expectedPasswordHash := sha256.Sum256([]byte(Conf.SyncAPI.APIPassword))

			usernameMatch := (subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1)
			passwordMatch := (subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1)

			if usernameMatch && passwordMatch {
				auth.ServeHTTP(w, r)

				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func ingest(w http.ResponseWriter, r *http.Request) {
	msg, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to read request body")

		return
	}
	defer r.Body.Close()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", Conf.Broker.SchemasPath), msg); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("eror on JSON validation: %s", err.Error()))

		return
	}

	if err := Conf.API.MQ.SendMessage(fmt.Sprintf("%v", time.Now().Format(time.RFC3339)), Conf.Broker.Exchange, Conf.SyncAPI.IngestRouting, msg); err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to send message: %s", err.Error()))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func accession(w http.ResponseWriter, r *http.Request) {
	msg, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to read request body")

		return
	}
	defer r.Body.Close()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", Conf.Broker.SchemasPath), msg); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("eror on JSON validation: %s", err.Error()))

		return
	}

	if err := Conf.API.MQ.SendMessage(fmt.Sprintf("%v", time.Now().Format(time.RFC3339)), Conf.Broker.Exchange, Conf.SyncAPI.AccessionRouting, msg); err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to send message: %s", err.Error()))

		return
	}

	w.WriteHeader(http.StatusOK)
}
