// The api service exposes an api through a set of http(s) endpoints to interface towards the sensitive-data-archive
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"log/slog"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	brokerv2 "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/broker/v2/rabbitmq"
	config "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/locationbroker"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type dataset struct {
	AccessionIDs []string `json:"accession_ids"`
	DatasetID    string   `json:"dataset_id"`
	User         string   `json:"user"`
}

//TODO: Wrap this in a struct?
var (
	auth        *userauth.ValidateFromToken
	inboxReader storage.Reader
	inboxWriter storage.Writer
	db          database.Database
	mq      brokerv2.Broker
)

func main() {
	if err := run(); err != nil {
		slog.Error("api server failed", "err", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := config.Load(); err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	db, err := postgres.NewPostgresSQLDatabase()
	if err != nil {
		return fmt.Errorf("failed to initialize sda db, due to: %v", err)
	}
	defer db.Close()
	if dbSchemaVersion, err := db.SchemaVersion(); err != nil || dbSchemaVersion < 23 {
		return errors.Join(errors.New("database schema v23 is required"), err)
	}

	mq, err = rabbitmq.NewRabbitMQBroker(context.Background())
	if err != nil {
		return fmt.Errorf("failed to initialize mq broker, due to: %v", err)
	}
	defer mq.Close()

	lb, err := locationbroker.NewLocationBroker(db)
	if err != nil {
		return fmt.Errorf("failed to initialize new location broker, due to: %v", err)
	}
	inboxWriter, err = storage.NewWriter(ctx, "inbox", lb)
	if err != nil {
		return fmt.Errorf("failed to initialize inbox writer, due to: %v", err)
	}
	inboxReader, err = storage.NewReader(ctx, "inbox")
	if err != nil {
		return fmt.Errorf("failed to initialize inbox reader, reason: %v", err)
	}

	if err := setupJwtAuth(); err != nil {
		return fmt.Errorf("error when setting up JWT auth, reason %s", err.Error())
	}

	serverErr := make(chan error, 1)
	addr := apiconfig.ApiAddr()
	srv, err := setup(addr)
	if err != nil {
		return fmt.Errorf("failed to setup http/https server, due to: %v", err)
	}
	go func() {
		if apiconfig.ServerCert() != "" && apiconfig.ServerKey() != "" {
			slog.Info("starting", "addr", addr)
			if err := srv.ListenAndServeTLS(apiconfig.ServerCert(), apiconfig.ServerKey()); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start https server, due to: %v", err)
			}
		} else {
			slog.Info("starting", "addr", addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("failed to start http server, due to: %v", err)
			}
		}
	}()
	defer func() {
		serverShutdownCtx, serverShutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := srv.Shutdown(serverShutdownCtx); err != nil {
			slog.Error("failed to close http/https server", "err", err)
		}
		serverShutdownCancel()
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-sigc:
		return nil
	case err := <-serverErr:
		return err
	}
}

func setup(addr string) (*http.Server, error) {
	m, _ := model.NewModelFromString(jsonadapter.Model)
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&apiconfig.RBACfile))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /ready", readinessResponse)
	// mux.HandleFunc("GET /files", rbac(e, getFiles))
	// mux.HandleFunc("POST /c4gh-keys/add", rbac(e, addC4ghHash))
	// mux.HandleFunc("GET /c4gh-keys/list", rbac(e, listC4ghHashes))
	// mux.HandleFunc("POST /c4gh-keys/deprecate/", rbac(e, deprecateC4ghHash)) // trailing slash matches prefix
	mux.HandleFunc("POST /file/ingest", rbac(e, ingestFile))
	mux.HandleFunc("DELETE /file/{username}/{fileid}", rbac(e, deleteFile))
	// mux.HandleFunc("POST /file/accession", rbac(e, setAccession))
	// mux.HandleFunc("PUT /file/verify/{accession}", rbac(e, reVerifyFile))
	// mux.HandleFunc("POST /file/rotatekey/{fileid}", rbac(e, rotateKeyFile))
	// mux.HandleFunc("GET /datasets", rbac(e, listDatasets))
	// mux.HandleFunc("GET /datasets/list", rbac(e, listAllDatasets))
	// mux.HandleFunc("GET /datasets/list/{username}", rbac(e, listUserDatasets))
	// mux.HandleFunc("POST /dataset/create", rbac(e, createDataset))
	// mux.HandleFunc("POST /dataset/rotatekey/{dataset}", rbac(e, rotateKeyDataset))
	// mux.HandleFunc("POST /dataset/release/", rbac(e, releaseDataset)) // trailing slash matches prefix
	// mux.HandleFunc("PUT /dataset/verify/", rbac(e, reVerifyDataset))  // trailing slash matches prefix
	// mux.HandleFunc("GET /users", rbac(e, listActiveUsers))
	// mux.HandleFunc("GET /users/{username}/files", rbac(e, listUserFiles))
	mux.HandleFunc("GET /users/{username}/file/{fileid}", rbac(e, downloadFile))

	var handler http.Handler = mux
	handler = recoveryMiddleware(handler)
	handler = loggingMiddleware(handler)

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      2 * time.Minute,
	}
	return srv, nil
}

func rbac(e *casbin.Enforcer, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error
		//TODO: add auth logic here
			if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
			}
			next(w, r)
	}
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "err", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	type responseWriter struct {
		http.ResponseWriter
		statusCode int
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rw, r)

		slog.LogAttrs(r.Context(),
			slog.LevelInfo,
			"request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
			slog.Int("status_code", rw.statusCode),
			slog.Duration("duration", time.Since(start)),
			slog.Time("time", start),
		)
	})
}

func setupJwtAuth() error {
	jwtPubKeyURL := apiconfig.JwtPubKeyURL()
	jwtPubKeyPath := apiconfig.JwtPubKeyPath()

	auth = userauth.NewValidateFromToken(jwk.NewSet())
	if jwtPubKeyURL != "" {
		if err := auth.FetchJwtPubKeyURL(jwtPubKeyURL); err != nil {
			return err
		}
	}

	if jwtPubKeyPath != "" {
		if err := auth.ReadJwtPubKeyPath(jwtPubKeyPath); err != nil {
			return err
		}
	}

	return nil
}

func readinessResponse(w http.ResponseWriter, r *http.Request) {
	if !mq.Alive() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unable to reach rabbitmq"))
	}

	if err := db.Ping(context.TODO()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unable to reach database"))
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func checkDB(ctx context.Context, db database.Database, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if db == nil {
		return errors.New("database is nil")
	}

	return db.Ping(ctx)
}

func abortWithJSON(w http.ResponseWriter, statusCode int, errorMessage string){
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{"error": errorMessage})
}

// parseLimitParam parses and validates the optional "limit" query parameter.
// It returns defaultPageLimit when the parameter is omitted or empty.
// It returns an error if the value is not a valid positive integer or exceeds maxPageLimit.
func parseLimitParam(limitStr string) (int, error) {
	const (
		defaultPageLimit = 1000
		maxPageLimit = 10000
	)
	if limitStr == "" {
		return defaultPageLimit, nil
	}
	li, err := strconv.Atoi(limitStr)
	if err != nil || li < 1 {
		return 0, errors.New("invalid limit parameter: must be a positive integer")
	}
	if li > maxPageLimit {
		return 0, fmt.Errorf("invalid limit parameter: must not exceed %d", maxPageLimit)
	}

	return li, nil
}

// getFiles returns the files from the database for a specific user
func getFiles(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "application/json")
	// Get user ID to extract all files
	token, err := auth.Authenticate(c.Request)
	if err != nil {
		// something went wrong with user token
		c.JSON(401, err.Error())

		return
	}

	// parse optional pagination params
	limit, err := parseLimitParam(c.Query("limit"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
	cursor := c.DefaultQuery("cursor", "")

	files, nextCursor, err := db.GetUserFiles(c, token.Subject(), c.Query("path_prefix"), false, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			c.AbortWithStatusJSON(http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		// something went wrong with querying or parsing rows
		c.JSON(502, err.Error())

		return
	}

	if nextCursor != "" {
		c.Header("X-Next-Cursor", nextCursor)
	}

	rsp := make([]*submissionFileInfo, len(files))

	for i, f := range files {
		rsp[i] = &submissionFileInfo{
			AccessionID:        f.AccessionID,
			FileID:             f.FileID,
			InboxPath:          f.InboxPath,
			Status:             f.Status,
			SubmissionFileSize: f.SubmissionFileSize,
			CreatedAt:          f.CreatedAt,
		}
	}

	// Return response
	c.JSON(200, rsp)
}

/*
ingestFile handles requests to initiate ingestion of a file.
This endpoint supports two input modes:
1. By file ID (via the "fileid" query parameter): Looks up the user and file path from the database.
2. By JSON payload: Expects a JSON body with user and file path.
The function constructs an ingest message, validates it
and sends it to the broker with the appropriate file ID.
*/
func ingestFile(w http.ResponseWriter, r *http.Request) {

	var (
		err error
		ingest schema.IngestionTrigger
		fileID string
	)

	fileID = r.URL.Query().Get("fileid")
	switch {
	case fileID != "" && r.ContentLength > 0:
		abortWithJSON(w, http.StatusBadRequest, "both file ID parameter and payload provided")

		return

	case r.URL.Query().Get("fileid") != "":
		if _, err := uuid.Parse(fileID); err != nil {
			abortWithJSON(w, http.StatusBadRequest, fmt.Sprintf("could not parse %s as uuid, reason: %v", fileID, err))

			return
		}

		fileDetails, err := db.GetFileDetails(context.TODO(), fileID, "uploaded")
		if err != nil {
			abortWithJSON(w, http.StatusBadRequest, fmt.Sprintf("could not find details for %s, reason: %v", fileID, err))

			return
		}

		ingest.User = fileDetails.User
		ingest.FilePath = fileDetails.Path

	case r.ContentLength > 0:
		if err = json.NewDecoder(r.Body).Decode(&ingest); err != nil {
			abortWithJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

			return
		}

		fileID, err = db.GetFileIDByUserPathAndStatus(context.TODO(), ingest.User, ingest.FilePath, "uploaded")
		if err != nil {
			abortWithJSON(w, http.StatusInternalServerError, err.Error())
		}

		if fileID == "" {
			abortWithJSON(w, http.StatusBadRequest, fmt.Sprintf("could not find fileID for %s", ingest.FilePath))

			return
		}

	default:
		abortWithJSON(w, http.StatusBadRequest, "missing parameter in payload")

		return
	}

	ingest.Type = "ingest"
	marshaledMsg, _ := json.Marshal(&ingest)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		abortWithJSON(w, http.StatusBadRequest, err.Error())

		return
	}

	//TODO: How do we create key and headers here?
	ingestMessage := brokerv2.Message{Key: "", Headers: nil, Body: marshaledMsg}
	err = mq.Publish(context.TODO(), "ingest", ingestMessage)
	if err != nil {
		abortWithJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

// The deleteFile function deletes files from the inbox and marks them as
// discarded in the db. Files are identified by their ids and the user id.
func deleteFile(w http.ResponseWriter, r *http.Request) {
	submissionUser := r.URL.Query().Get("username")
	slog.Debug("submission", "user", submissionUser)

	fileID := r.URL.Query().Get("fileid")
	fileID = strings.TrimPrefix(fileID, "/")
	slog.Debug("recieved file for deletion", "file_id", fileID)
	if fileID == "" {
		abortWithJSON(w, http.StatusBadRequest, "file ID is requiered")

		return
	}

	// Get the file path from the fileID and submission user
	filePath, location, err := db.GetUploadedSubmissionFilePathAndLocation(r.Context(), submissionUser, fileID)
	if err != nil {
		slog.Error("could not get file from fileID, reason: %v", err)
		abortWithJSON(w, http.StatusNotFound, "file could not be found in inbox")

		return
	}

	if location == "" {
		slog.Error("no known submission location found", "file_id", fileID)
		abortWithJSON(w, http.StatusInternalServerError, "failed to find file in location")

		return
	}

	filePath = helper.UnanonymizeFilepath(filePath, submissionUser)
	for count := 1; count <= 5; count++ {
		err = inboxWriter.RemoveFile(r.Context(), location, filePath)
		if err == nil {
			break
		}

		slog.Error("failed to remove file from inbox", "err", err)
		if count == 5 {
		abortWithJSON(w, http.StatusInternalServerError, "failed to remove file from inbox")

			return
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	if err := db.UpdateFileEventLog(r.Context(), fileID, "disabled", "api", "{}", "{}"); err != nil {
		slog.Error("set status deleted failed", "err", err)
		abortWithJSON(w, http.StatusInternalServerError, err.Error())

		return
	}


	w.Write([]byte(string(http.StatusOK)))
}

// reencryptHeader re-encrypts the header of a file using the public key
// provided in the request header and returns the new header. The function uses
// gRPC to communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate is
// provided for secure communication.
func reencryptHeader(ctx context.Context, oldHeader []byte, c4ghPubKey string) ([]byte, error) {
	var opts []grpc.DialOption
	grpcClient, err := apiconfig.GrpcClient()
	if err != nil {
		return nil, err
	}

	switch {
	case grpcClient.ClientCreds != nil:
		opts = append(opts, grpc.WithTransportCredentials(grpcClient.ClientCreds))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(apiconfig.GrpcAddr(), opts...)
	if err != nil {
		slog.Error("failed to connect to reencrypt service", "err", err)

		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(grpcClient.Timeout)*time.Second)
	defer cancel()

	c := reencrypt.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{Oldheader: oldHeader, Publickey: c4ghPubKey})
	if err != nil {
		return nil, err
	}

	return res.Header, nil
}

// Download a file re-encrypted with the public key provided in the request header from the inbox and retrieves the file path
// from the database using the file ID and user ID.
func downloadFile(w http.ResponseWriter, r *http.Request) {
	c4ghPubKey := r.Header.Get("C4GH-Public-Key")

	pubKey, err := base64.StdEncoding.DecodeString(c4ghPubKey)
	if err != nil || len(pubKey) == 0 {
		slog.Error("could not decode c4gh public key", "err", err)
		abortWithJSON(w, http.StatusBadRequest, "bad public key")

		return
	}

	fileID := r.URL.Query().Get("fileid")
	fileID = strings.TrimPrefix(fileID, "/")
	submissionUser := r.URL.Query().Get("username")
	submissionUser = strings.TrimPrefix(submissionUser, "/")
	filePath, location, err := db.GetUploadedSubmissionFilePathAndLocation(r.Context(), submissionUser, fileID)
	if err != nil {
		slog.Error("failed to get filepath from fileID", "file_id", fileID, "err", err)
		abortWithJSON(w, http.StatusNotFound, "failed to retrieve inbox file path")

		return
	}
	if location == "" {
		slog.Error("fileID has no known submission location", "file_id", fileID, "err", err)
		abortWithJSON(w, http.StatusInternalServerError, "failed to find file location")

		return
	}

	file, err := inboxReader.NewFileReader(r.Context(), location, helper.UnanonymizeFilepath(filePath, submissionUser))
	if err != nil {
		slog.Error("inbox file not found or failed to read", "file_path", filePath, "err", err)
		abortWithJSON(w, http.StatusInternalServerError, "failed to read inbox file")

		return
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := headers.ReadHeader(file)
	if err != nil {
		slog.Error("failed to read header for fiel", "file_id", fileID, "err", err)
		abortWithJSON(w, http.StatusInternalServerError, "failed to read the start of the file")

		return
	}

	newHeader, err := reencryptHeader(r.Context(), header, c4ghPubKey)
	if err != nil {
		slog.Error("failed to reencrypt header", "err", err)
		abortWithJSON(w, http.StatusInternalServerError, "failed to reencrypt header")

		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", path.Base(filePath)))

	reader := io.MultiReader(bytes.NewReader(newHeader), file)
	_, err = io.Copy(w, reader)
	if err != nil {
		slog.Error("error occurred while sending stream", "err", err)
		abortWithJSON(w, http.StatusInternalServerError, "failed to stream data to client")

		return
	}

	w.WriteHeader(http.StatusOK)
}

/*
setAccession handles requests to assign an accession ID to a file.
This endpoint supports two input modes:
1. By query parameters ("fileid" and "accessionid"): Retrieves user, file path, and decrypted checksum from the database using the file ID.
2. By JSON payload: Expects a JSON body with user and file path, then looks up the file ID and decrypted checksum.
If both query parameters and a JSON payload are provided, the request is rejected with a 400 Bad Request.
The function constructs an accession message, validates it and sends it to the message broker.
*/
func setAccession(w http.ResponseWriter, r *http.Request) {
	var accession schema.IngestionAccession
	fileID := r.URL.Query().Get("fileid")
	accessionID := r.URL.Query().Get("accessionid")
	hasQuery := fileID != "" || accessionID != ""
	missingAccession := fileID != "" && accessionID == ""
	hasBody := r.ContentLength > 0
	switch {
	case hasQuery && hasBody:
		abortWithJSON(w, http.StatusBadRequest, "recieved both query parameters and json payload")

		return
	case missingAccession:
		abortWithJSON(w, http.StatusBadRequest, "accessionid not provided")

		return
	case fileID != "" && accessionID != "":
		if _, err := uuid.Parse(fileID); err != nil {
		abortWithJSON(w, http.StatusBadRequest, "provided fileid could not be parsed as valid uuid")

			return
		}

		fileDetails, err := db.GetFileDetails(r.Context(), fileID, "verified")
		if err != nil {
			abortWithJSON(w, http.StatusBadRequest, "file details not found")

			return
		}

		fileDecrChecksum, err := db.GetDecryptedChecksum(r.Context(), fileID)
		if err != nil {
			slog.Debug("failed to decrypt checksum from database", "err", err)
			abortWithJSON(w, http.StatusInternalServerError, "failed to get decrypted checksuom from database")

			return
		}

		accession.AccessionID = accessionID
		accession.User = fileDetails.User
		accession.FilePath = fileDetails.Path
		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	//TODO: Continue from here
	case r.ContentLength > 0:
		if err := c.BindJSON(&accession); err != nil {
			c.AbortWithStatusJSON(
				http.StatusBadRequest,
				gin.H{
					"error":  "json decoding : " + err.Error(),
					"status": http.StatusBadRequest,
				},
			)

			return
		}
		fileID, err = db.GetFileIDByUserPathAndStatus(c, accession.User, accession.FilePath, "verified")
		if err != nil {
			if fileID == "" {
				c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())
			} else {
				c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())
			}

			return
		}
		// Get decrypted checksum
		fileDecrChecksum, err := db.GetDecryptedChecksum(c, fileID)
		if err != nil {
			log.Debugln(err.Error())
			c.AbortWithStatusJSON(http.StatusNotFound, "decrypted checksum not found")

			return
		}
		// Add decrypted checksum in message payload
		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, "missing parameter or payload")

		return
	}
	// Add type in the message payload
	accession.Type = "accession"

	marshaledMsg, _ := json.Marshal(&accession)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}

	err = Conf.API.MQ.SendMessage(fileID, Conf.Broker.Exchange, "accession", marshaledMsg)
	if err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Status(http.StatusOK)
}

func createDataset(c *gin.Context) {
	var dataset dataset
	if err := c.BindJSON(&dataset); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			gin.H{
				"error":  "json decoding : " + err.Error(),
				"status": http.StatusBadRequest,
			},
		)

		return
	}

	if len(dataset.AccessionIDs) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, "at least one accessionID is required")

		return
	}

	// Check that the files the accession ids are linked to belong to the user of the dataset
	for _, accessionID := range dataset.AccessionIDs {
		belongsToUser, err := db.CheckAccessionIDOwnedByUser(c, accessionID, dataset.User)
		if err != nil {
			log.Errorln(err.Error())
			c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

			return
		}
		if !belongsToUser {
			c.AbortWithStatusJSON(http.StatusBadRequest, fmt.Sprintf("accession ID: %s not found or owned by other user", accessionID))

			return
		}
	}

	mapping := schema.DatasetMapping{
		Type:         "mapping",
		AccessionIDs: dataset.AccessionIDs,
		DatasetID:    dataset.DatasetID,
	}
	marshaledMsg, _ := json.Marshal(&mapping)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}

	err = Conf.API.MQ.SendMessage("", Conf.Broker.Exchange, "mappings", marshaledMsg)
	if err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Status(http.StatusOK)
}

func releaseDataset(c *gin.Context) {
	datasetID := strings.TrimPrefix(c.Param("dataset"), "/")
	ok, err := db.CheckIfDatasetExists(c, datasetID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
	if !ok {
		c.AbortWithStatusJSON(http.StatusNotFound, "dataset not found")

		return
	}

	status, err := db.GetDatasetStatus(c, datasetID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
	if status != "registered" {
		c.AbortWithStatusJSON(http.StatusBadRequest, fmt.Sprintf("dataset already %s", status))

		return
	}

	datasetMsg := schema.DatasetRelease{
		Type:      "release",
		DatasetID: datasetID,
	}
	marshaledMsg, _ := json.Marshal(&datasetMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}

	err = Conf.API.MQ.SendMessage("", Conf.Broker.Exchange, "mappings", marshaledMsg)
	if err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Status(http.StatusOK)
}

// rotateKeyFile triggers key rotation for a specific file
func rotateKeyFile(c *gin.Context) {
	fileID := c.Param("fileid")

	if fileID == "" {
		c.JSON(http.StatusBadRequest, "file ID is required")

		return
	}

	// Create rotation message
	rotateMsg := schema.KeyRotation{
		Type:   "key_rotation",
		FileID: fileID,
	}

	marshaledMsg, err := json.Marshal(&rotateMsg)
	if err != nil {
		log.Errorf("failed to marshal rotation message, reason: %v", err)
		c.JSON(http.StatusInternalServerError, "failed to marshal rotation message")

		return
	}

	// Validate the message against schema
	if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Errorf("rotation message validation failed, reason: %v", err)
		c.JSON(http.StatusBadRequest, "file ID not a proper UUID")

		return
	}

	// Send message to rotatekey queue
	err = Conf.API.MQ.SendMessage("", Conf.Broker.Exchange, "rotatekey", marshaledMsg)
	if err != nil {
		log.Errorf("failed to send rotation message to queue, reason: %v", err)
		c.JSON(http.StatusInternalServerError, "failed to send message")

		return
	}

	c.Status(http.StatusOK)
}

// rotateKeyDataset triggers key rotation for all files in a dataset
func rotateKeyDataset(c *gin.Context) {
	datasetID := c.Param("dataset")

	if datasetID == "" {
		c.JSON(http.StatusBadRequest, "dataset ID is required")

		return
	}

	// Check if dataset exists
	exists, err := db.CheckIfDatasetExists(c, datasetID)
	if err != nil {
		log.Errorf("failed to check if dataset %s exists, reason: %v", datasetID, err)
		c.JSON(http.StatusInternalServerError, "failed to check dataset existence")

		return
	}
	if !exists {
		log.Warnf("dataset %s not found", datasetID)
		c.JSON(http.StatusNotFound, fmt.Sprintf("dataset %s not found", datasetID))

		return
	}

	// Get all files in the dataset
	files, err := db.GetDatasetFileIDs(c, datasetID)
	if err != nil {
		log.Errorf("failed to get dataset files for dataset %s, reason: %v", datasetID, err)
		c.JSON(http.StatusInternalServerError, "failed to get dataset files")

		return
	}

	if len(files) == 0 {
		log.Warnf("no files found for dataset %s", datasetID)
		c.AbortWithStatusJSON(http.StatusBadRequest, "dataset not found")

		return
	}

	// Send rotation message for each file in the dataset
	for _, fileID := range files {
		// Create rotation message
		rotateMsg := schema.KeyRotation{
			Type:   "key_rotation",
			FileID: fileID,
		}

		marshaledMsg, err := json.Marshal(&rotateMsg)
		if err != nil {
			log.Errorf("failed to marshal rotation message for file %s, reason: %v", fileID, err)
			c.JSON(http.StatusInternalServerError, "failed to marshal rotation message")

			return
		}

		// Validate the message against schema
		if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
			log.Errorf("rotation message validation failed for file %s, reason: %v", fileID, err)
			c.JSON(http.StatusInternalServerError, "rotation message validation failed")

			return
		}

		// Send message to rotatekey queue
		err = Conf.API.MQ.SendMessage("", Conf.Broker.Exchange, "rotatekey", marshaledMsg)
		if err != nil {
			log.Errorf("failed to send rotation message for file %s to queue, reason: %v", fileID, err)
			c.JSON(http.StatusInternalServerError, "failed to send rotation message")

			return
		}
	}

	log.Infof("rotation messages sent for %d files in dataset %s", len(files), datasetID)
	c.Status(http.StatusOK)
}

func listActiveUsers(c *gin.Context) {
	users, err := db.ListActiveUsers(c)
	if err != nil {
		log.Debugln("ListActiveUsers failed")
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	c.JSON(http.StatusOK, users)
}

// listUserFiles returns a list of files for a specific user
// If the file has status disabled, the file will be skipped
func listUserFiles(c *gin.Context) {
	username := c.Param("username")
	username = strings.TrimPrefix(username, "/")
	username = strings.TrimSuffix(username, "/files")
	log.Debugln(username)

	// parse optional pagination params
	limit, err := parseLimitParam(c.Query("limit"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
	cursor := c.DefaultQuery("cursor", "")
	files, nextCursor, err := db.GetUserFiles(c, username, c.Query("path_prefix"), true, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			c.AbortWithStatusJSON(http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	if nextCursor != "" {
		c.Header("X-Next-Cursor", nextCursor)
	}

	rsp := make([]*submissionFileInfo, len(files))

	for i, f := range files {
		rsp[i] = &submissionFileInfo{
			AccessionID:        f.AccessionID,
			FileID:             f.FileID,
			InboxPath:          f.InboxPath,
			Status:             f.Status,
			SubmissionFileSize: f.SubmissionFileSize,
			CreatedAt:          f.CreatedAt,
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.JSON(200, rsp)
}

// addC4ghHash handles the addition of a hashed public key to the database.
// It expects a JSON payload containing the base64 encoded public key and its description.
// If the JSON payload is invalid, it responds with a 400 Bad Request status.
// If the hash is already in the database, it responds with a 409 Conflict status
// If the database insertion fails, it responds with a 500 Internal Server Error status.
// On success, it responds with a 200 OK status.
func addC4ghHash(c *gin.Context) {
	var c4gh schema.C4ghPubKey
	if err := c.BindJSON(&c4gh); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			gin.H{
				"error":  "json decoding : " + err.Error(),
				"status": http.StatusBadRequest,
			},
		)

		log.Errorf("Invalid JSON payload: %v", err)

		return
	}

	b64d, err := base64.StdEncoding.DecodeString(c4gh.PubKey)
	if err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			gin.H{
				"error":  "base64 decoding : " + err.Error(),
				"status": http.StatusBadRequest,
			},
		)

		log.Errorf("Invalid JSON payload: %v", err)

		return
	}

	pubKey, err := keys.ReadPublicKey(bytes.NewReader(b64d))
	if err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			gin.H{
				"error":  "not a public key : " + err.Error(),
				"status": http.StatusBadRequest,
			},
		)

		log.Errorf("Invalid JSON payload: %v", err)

		return
	}

	err = db.AddKeyHash(c, hex.EncodeToString(pubKey[:]), c4gh.Description)
	if err != nil {
		if strings.Contains(err.Error(), "key hash already exists") {
			c.AbortWithStatusJSON(
				http.StatusConflict,
				gin.H{
					"error":  err.Error(),
					"status": http.StatusConflict,
				},
			)
			log.Error("Key hash already exists")
		} else {
			c.AbortWithStatusJSON(
				http.StatusInternalServerError,
				gin.H{
					"error":  err.Error(),
					"status": http.StatusInternalServerError,
				},
			)
			log.Errorf("Database insertion failed: %v", err)
		}

		return
	}

	c.Status(http.StatusOK)
}

func listC4ghHashes(c *gin.Context) {
	hashes, err := db.ListKeyHashes(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	for n, h := range hashes {
		ct, _ := time.Parse(time.RFC3339, h.CreatedAt)
		hashes[n].CreatedAt = ct.Format(time.DateTime)

		if h.DeprecatedAt != "" {
			dt, _ := time.Parse(time.RFC3339, h.DeprecatedAt)
			hashes[n].DeprecatedAt = dt.Format(time.DateTime)
		}
	}
	c.Writer.Header().Set("Content-Type", "application/json")

	rsp := make([]*c4ghKeyHash, len(hashes))

	for i, hash := range hashes {
		rsp[i] = &c4ghKeyHash{
			Hash:         hash.Hash,
			Description:  hash.Description,
			CreatedAt:    hash.CreatedAt,
			DeprecatedAt: hash.DeprecatedAt,
		}
	}

	c.JSON(200, rsp)
}

func deprecateC4ghHash(c *gin.Context) {
	keyHash := strings.TrimPrefix(c.Param("keyHash"), "/")
	err = db.DeprecateKeyHash(c, keyHash)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
}

func listAllDatasets(c *gin.Context) {
	datasets, err := db.ListDatasets(c)
	if err != nil {
		log.Errorf("ListAllDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	rsp := make([]*datasetInfo, len(datasets))

	for i, d := range datasets {
		rsp[i] = &datasetInfo{
			DatasetID: d.DatasetID,
			Status:    d.Status,
			Timestamp: d.Timestamp,
		}
	}

	c.JSON(http.StatusOK, rsp)
}

func listUserDatasets(c *gin.Context) {
	username := strings.TrimPrefix(c.Param("username"), "/")
	datasets, err := db.ListUserDatasets(c, username)
	if err != nil {
		log.Errorf("ListUserDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	rsp := make([]*datasetInfo, len(datasets))

	for i, d := range datasets {
		rsp[i] = &datasetInfo{
			DatasetID: d.DatasetID,
			Status:    d.Status,
			Timestamp: d.Timestamp,
		}
	}

	c.JSON(http.StatusOK, rsp)
}

func listDatasets(c *gin.Context) {
	token, err := auth.Authenticate(c.Request)
	if err != nil {
		c.JSON(401, err.Error())

		return
	}
	datasets, err := db.ListUserDatasets(c, token.Subject())
	if err != nil {
		log.Errorf("ListDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	rsp := make([]*datasetInfo, len(datasets))

	for i, d := range datasets {
		rsp[i] = &datasetInfo{
			DatasetID: d.DatasetID,
			Status:    d.Status,
			Timestamp: d.Timestamp,
		}
	}

	c.JSON(http.StatusOK, rsp)
}

func reVerify(c *gin.Context, accessionID string) (*gin.Context, error) {
	reverificationData, err := db.GetReVerificationData(c, accessionID)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			c.AbortWithStatusJSON(http.StatusNotFound, "accession ID not found")
		} else {
			log.Errorln("failed to get file data")
			c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())
		}

		return c, err
	}
	reVerifyMsg := schema.IngestionVerification{
		User:        reverificationData.SubmissionUser,
		FilePath:    reverificationData.SubmissionFilePath,
		FileID:      reverificationData.FileID,
		ArchivePath: reverificationData.ArchiveFilePath,
		EncryptedChecksums: []schema.Checksums{{
			Type:  reverificationData.ArchivedCheckSumType,
			Value: reverificationData.ArchivedCheckSum,
		}},
		ReVerify: true,
	}
	marshaledMsg, _ := json.Marshal(&reVerifyMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Errorln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return c, err
	}

	err = Conf.API.MQ.SendMessage(reVerifyMsg.FileID, Conf.Broker.Exchange, "archived", marshaledMsg)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return c, err
	}

	return c, nil
}

func reVerifyFile(c *gin.Context) {
	accessionID := strings.TrimPrefix(c.Param("accession"), "/")
	c, err = reVerify(c, accessionID)
	if err != nil {
		return
	}

	c.Status(http.StatusOK)
}

func reVerifyDataset(c *gin.Context) {
	dataset := strings.TrimPrefix(c.Param("dataset"), "/")
	files, err := db.GetDatasetFiles(c, dataset)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	if files == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, "dataset not found")

		return
	}

	for _, accession := range files {
		c, err = reVerify(c, accession)
		if err != nil {
			return
		}
	}

	c.Status(http.StatusOK)
}
