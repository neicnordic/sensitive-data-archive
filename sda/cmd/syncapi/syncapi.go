package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"

	log "github.com/sirupsen/logrus"
)

var Conf *config.Config
var err error

type syncDataset struct {
	DatasetID    string         `json:"dataset_id"`
	DatasetFiles []datasetFiles `json:"dataset_files"`
	User         string         `json:"user"`
}

type datasetFiles struct {
	FilePath string `json:"filepath"`
	FileID   string `json:"file_id"`
	ShaSum   string `json:"sha256"`
}

func main() {
	Conf, err = config.NewConfig("sync-api")
	if err != nil {
		log.Fatal(err)
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
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

	go func() {
		forever := make(chan bool)
		messages, err := Conf.API.MQ.GetMessages(Conf.Broker.Queue)
		if err != nil {
			log.Fatal(err)
		}
		for m := range messages {
			log.Debugf("Received a message (corr-id: %s, message: %s)", m.CorrelationId, m.Body)
			err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", Conf.Broker.SchemasPath), m.Body)
			if err != nil {
				log.Errorf("validation of incoming message (dataset-mapping) failed, reason: (%s)", err.Error())
				// Send the message to an error queue so it can be analyzed.
				infoErrorMessage := broker.InfoError{
					Error:           "Message validation failed",
					Reason:          err.Error(),
					OriginalMessage: m,
				}

				body, _ := json.Marshal(infoErrorMessage)
				if err := Conf.API.MQ.SendMessage(m.CorrelationId, Conf.Broker.Exchange, "error", body); err != nil {
					log.Errorf("failed to publish message, reason: (%s)", err.Error())
				}
				if err := m.Ack(false); err != nil {
					log.Errorf("failed to Ack message, reason: (%s)", err.Error())
				}

				continue
			}

			log.Infoln("buildSyncDatasetJSON")
			blob, err := buildSyncDatasetJSON(m.Body)
			if err != nil {
				log.Errorf("failed to build SyncDatasetJSON, Reason: %v", err)
			}
			if err := sendPOST(blob); err != nil {
				log.Errorf("failed to send POST, Reason: %v", err)
			}
			if err := m.Ack(false); err != nil {
				log.Errorf("Failed to ack message: reason %v", err)
			}

		}
		<-forever
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
	r.HandleFunc("/dataset", basicAuth(http.HandlerFunc(dataset))).Methods("POST")
	r.HandleFunc("/metadata", basicAuth(http.HandlerFunc(metadata))).Methods("POST")

	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	srv := &http.Server{
		Addr:              config.API.Host + ":" + fmt.Sprint(config.API.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
	}

	return srv
}

func shutdown() {
	defer Conf.API.MQ.Channel.Close()
	defer Conf.API.MQ.Connection.Close()
	defer Conf.API.DB.Close()
}

func readinessResponse(w http.ResponseWriter, r *http.Request) {
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

	if DBRes := checkDB(Conf.API.DB, 5*time.Millisecond); DBRes != nil {
		log.Debugf("DB connection error :%v", DBRes)
		Conf.API.DB.Connect()
		statusCocde = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCocde)
}

func checkDB(database *database.SDAdb, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if database.DB == nil {
		return fmt.Errorf("database is nil")
	}

	return database.DB.PingContext(ctx)
}

func dataset(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to read request body")

		return
	}
	defer r.Body.Close()

	if err := schema.ValidateJSON(fmt.Sprintf("%s/../bigpicture/file-sync.json", Conf.Broker.SchemasPath), b); err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("eror on JSON validation: %s", err.Error()))

		return
	}

	if err := parseDatasetMessage(b); err != nil {
		if err.Error() == "Dataset exists" {
			w.WriteHeader(http.StatusAlreadyReported)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// parsemessage parses the JSON blob and sends the relevant messages
func parseDatasetMessage(msg []byte) error {
	blob := syncDataset{}
	_ = json.Unmarshal(msg, &blob)

	ds, err := Conf.API.DB.CheckIfDatasetExists(blob.DatasetID)
	if err != nil {
		return fmt.Errorf("Failed to check dataset existance: Reason %v", err)
	}
	if ds {
		return fmt.Errorf("Dataset exists")
	}

	var accessionIDs []string
	for _, files := range blob.DatasetFiles {
		ingest := schema.IngestionTrigger{
			Type:     "ingest",
			User:     blob.User,
			FilePath: files.FilePath,
		}
		ingestMsg, err := json.Marshal(ingest)
		if err != nil {
			return fmt.Errorf("Failed to marshal json messge: Reason %v", err)
		}

		if err := Conf.API.MQ.SendMessage(fmt.Sprintf("%v", time.Now().Unix()), Conf.Broker.Exchange, "ingest", ingestMsg); err != nil {
			return fmt.Errorf("Failed to send ingest messge: Reason %v", err)
		}

		accessionIDs = append(accessionIDs, files.FileID)
		finalize := schema.IngestionAccession{
			Type:               "accession",
			User:               blob.User,
			FilePath:           files.FilePath,
			AccessionID:        files.FileID,
			DecryptedChecksums: []schema.Checksums{{Type: "sha256", Value: files.ShaSum}},
		}
		finalizeMsg, err := json.Marshal(finalize)
		if err != nil {
			return fmt.Errorf("Failed to marshal json messge: Reason %v", err)
		}

		if err := Conf.API.MQ.SendMessage(fmt.Sprintf("%v", time.Now().Unix()), Conf.Broker.Exchange, "accession", finalizeMsg); err != nil {
			return fmt.Errorf("Failed to send mapping messge: Reason %v", err)
		}
	}

	mappings := schema.DatasetMapping{
		Type:         "mapping",
		DatasetID:    blob.DatasetID,
		AccessionIDs: accessionIDs,
	}
	mappingMsg, err := json.Marshal(mappings)
	if err != nil {
		return fmt.Errorf("Failed to marshal json messge: Reason %v", err)
	}

	if err := Conf.API.MQ.SendMessage(fmt.Sprintf("%v", time.Now().Unix()), Conf.Broker.Exchange, "mappings", mappingMsg); err != nil {
		return fmt.Errorf("Failed to send mapping messge: Reason %v", err)
	}

	return nil
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

func buildSyncDatasetJSON(b []byte) ([]byte, error) {
	var msg schema.DatasetMapping
	_ = json.Unmarshal(b, &msg)

	var dataset = syncDataset{
		DatasetID: msg.DatasetID,
	}

	for _, ID := range msg.AccessionIDs {
		data, err := Conf.API.DB.GetSyncData(ID)
		if err != nil {
			return nil, err
		}
		datasetFile := datasetFiles{
			FilePath: data.FilePath,
			FileID:   ID,
			ShaSum:   data.Checksum,
		}
		dataset.DatasetFiles = append(dataset.DatasetFiles, datasetFile)
		dataset.User = data.User
	}

	json, err := json.Marshal(dataset)
	if err != nil {
		return nil, err
	}

	return json, nil
}

func sendPOST(payload []byte) error {
	client := &http.Client{}
	URL, err := createHostURL(Conf.SyncAPI.RemoteHost, Conf.SyncAPI.RemotePort)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(Conf.SyncAPI.RemoteUser, Conf.SyncAPI.RemotePassword)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func createHostURL(host string, port int) (string, error) {
	url, err := url.ParseRequestURI(host)
	if err != nil {
		return "", err
	}
	if url.Port() == "" && port != 0 {
		url.Host += fmt.Sprintf(":%d", port)
	}
	url.Path = "/dataset"

	return url.String(), nil
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
