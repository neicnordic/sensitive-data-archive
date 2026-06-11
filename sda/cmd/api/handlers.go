package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func (api *API) rbac(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := api.auth.Authenticate(r)
		if err != nil {
			slog.Error("failed to authorize request", "err", err)
			writeErrorStatus(w, http.StatusUnauthorized, "failed to authorize request")

			return
		}

		subject := token.Subject()
		urlPath := r.URL.Path
		method := r.Method

		ok, err := api.enforcer.Enforce(subject, urlPath)
		if err != nil {
			slog.Error("failed to enforce subject for the request url", "subject", subject, "method", method, "url_path", urlPath)
			writeErrorStatus(w, http.StatusInternalServerError, "failed to enforce subject for the requested url")

			return
		}

		if !ok {
			slog.Warn("unathorized request", "subject", subject, "method", method, "url_path", urlPath)
			writeErrorStatus(w, http.StatusUnauthorized, "not authorized")

			return
		}

		slog.Info("handliing request", "subject", subject, "method", method, "url_path", urlPath)
		next(w, r)
	}
}

func (api *API) recoveryMiddleware(next http.Handler) http.Handler {
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

func (api *API) loggingMiddleware(next http.Handler) http.Handler {
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

func (api *API) setupJwtAuth() error {
	jwtPubKeyURL := apiconfig.JwtPubKeyURL()
	jwtPubKeyPath := apiconfig.JwtPubKeyPath()

	api.auth = userauth.NewValidateFromToken(jwk.NewSet())
	if jwtPubKeyURL != "" {
		if err := api.auth.FetchJwtPubKeyURL(jwtPubKeyURL); err != nil {
			return err
		}
	}

	if jwtPubKeyPath != "" {
		if err := api.auth.ReadJwtPubKeyPath(jwtPubKeyPath); err != nil {
			return err
		}
	}

	return nil
}

func (api *API) readinessResponse(w http.ResponseWriter, r *http.Request) {
	if !api.mq.Alive() {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unable to reach rabbitmq"))
	}

	if err := api.db.Ping(context.TODO()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unable to reach database"))
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func writeErrorStatus(w http.ResponseWriter, statusCode int, errorMessage string) {
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
		maxPageLimit     = 10000
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

func (api *API) getFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	token, err := api.auth.Authenticate(r)
	if err != nil {
		slog.Error("failed to authenticate user", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to authenticate user")

		return
	}

	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		slog.Error("failed to parse pagination limit", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, err.Error())

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	files, nextCursor, err := api.db.GetUserFiles(r.Context(), token.Subject(), pathPrefix, false, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			slog.Error("invalid cursor parameter", "cursor", cursor, "limig", limit, "path_prefix", pathPrefix, "err", err)
			writeErrorStatus(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	if nextCursor != "" {
		w.Header().Set("X-Next-Cursor", nextCursor)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rsp)
}

/*
ingestFile handles requests to initiate ingestion of a file.
This endpoint supports two input modes:
1. By file ID (via the "fileid" query parameter): Looks up the user and file path from the database.
2. By JSON payload: Expects a JSON body with user and file path.
The function constructs an ingest message, validates it
and sends it to the broker with the appropriate file ID.
*/
func (api *API) ingestFile(w http.ResponseWriter, r *http.Request) {

	var (
		err    error
		ingest schema.IngestionTrigger
		fileID string
	)

	fileID = r.URL.Query().Get("fileid")
	switch {
	case fileID != "" && r.ContentLength > 0:
		writeErrorStatus(w, http.StatusBadRequest, "both file ID parameter and payload provided")

		return

	case r.URL.Query().Get("fileid") != "":
		if _, err := uuid.Parse(fileID); err != nil {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not parse %s as uuid, reason: %v", fileID, err))

			return
		}

		fileDetails, err := api.db.GetFileDetails(context.TODO(), fileID, "uploaded")
		if err != nil {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not find details for %s, reason: %v", fileID, err))

			return
		}

		ingest.User = fileDetails.User
		ingest.FilePath = fileDetails.Path

	case r.ContentLength > 0:
		if err = json.NewDecoder(r.Body).Decode(&ingest); err != nil {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

			return
		}

		fileID, err = api.db.GetFileIDByUserPathAndStatus(context.TODO(), ingest.User, ingest.FilePath, "uploaded")
		if err != nil {
			writeErrorStatus(w, http.StatusInternalServerError, err.Error())
		}

		if fileID == "" {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not find fileID for %s", ingest.FilePath))

			return
		}

	default:
		writeErrorStatus(w, http.StatusBadRequest, "missing parameter in payload")

		return
	}

	ingest.Type = "ingest"
	marshaledMsg, _ := json.Marshal(&ingest)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate ingestion message", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate ingestion message, reason: %v", err))

		return
	}

	//TODO: How do we create key and headers here?
	ingestMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "ingest", ingestMessage); err != nil {
		slog.Debug("failed to publish ingest message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

// The deleteFile function deletes files from the inbox and marks them as
// discarded in the db. Files are identified by their ids and the user id.
func (api *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	submissionUser := r.URL.Query().Get("username")
	slog.Debug("submission", "user", submissionUser)

	fileID := r.URL.Query().Get("fileid")
	fileID = strings.TrimPrefix(fileID, "/")
	slog.Debug("recieved file for deletion", "file_id", fileID)
	if fileID == "" {
		writeErrorStatus(w, http.StatusBadRequest, "file ID is requiered")

		return
	}

	// Get the file path from the fileID and submission user
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(r.Context(), submissionUser, fileID)
	if err != nil {
		slog.Error("file could not be found in inbox", "file_id", fileID, "err", err)
		writeErrorStatus(w, http.StatusNotFound, "file could not be found in inbox")

		return
	}

	if location == "" {
		slog.Error("no known submission location found", "file_id", fileID)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to find file in location")

		return
	}

	filePath = helper.UnanonymizeFilepath(filePath, submissionUser)
	for count := 1; count <= 5; count++ {
		err = api.inboxWriter.RemoveFile(r.Context(), location, filePath)
		if err == nil {
			break
		}

		slog.Error("failed to remove file from inbox", "err", err)
		if count == 5 {
			writeErrorStatus(w, http.StatusInternalServerError, "failed to remove file from inbox")

			return
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	if err := api.db.UpdateFileEventLog(r.Context(), fileID, "disabled", "api", "{}", "{}"); err != nil {
		slog.Error("set status deleted failed", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

// reencryptHeader re-encrypts the header of a file using the public key
// provided in the request header and returns the new header. The function uses
// gRPC to communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate is
// provided for secure communication.
func (api *API) reencryptHeader(ctx context.Context, oldHeader []byte, c4ghPubKey string) ([]byte, error) {
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
func (api *API) downloadFile(w http.ResponseWriter, r *http.Request) {
	c4ghPubKey := r.Header.Get("C4GH-Public-Key")

	pubKey, err := base64.StdEncoding.DecodeString(c4ghPubKey)
	if err != nil || len(pubKey) == 0 {
		slog.Error("could not decode c4gh public key", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, "bad public key")

		return
	}

	fileID := r.URL.Query().Get("fileid")
	fileID = strings.TrimPrefix(fileID, "/")
	submissionUser := r.URL.Query().Get("username")
	submissionUser = strings.TrimPrefix(submissionUser, "/")
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(r.Context(), submissionUser, fileID)
	if err != nil {
		slog.Error("failed to get filepath from fileID", "file_id", fileID, "err", err)
		writeErrorStatus(w, http.StatusNotFound, "failed to retrieve inbox file path")

		return
	}
	if location == "" {
		slog.Error("fileID has no known submission location", "file_id", fileID, "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to find file location")

		return
	}

	file, err := api.inboxReader.NewFileReader(r.Context(), location, helper.UnanonymizeFilepath(filePath, submissionUser))
	if err != nil {
		slog.Error("inbox file not found or failed to read", "file_path", filePath, "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to read inbox file")

		return
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := headers.ReadHeader(file)
	if err != nil {
		slog.Error("failed to read header for fiel", "file_id", fileID, "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to read the start of the file")

		return
	}

	newHeader, err := api.reencryptHeader(r.Context(), header, c4ghPubKey)
	if err != nil {
		slog.Error("failed to reencrypt header", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to reencrypt header")

		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", path.Base(filePath)))

	reader := io.MultiReader(bytes.NewReader(newHeader), file)
	_, err = io.Copy(w, reader)
	if err != nil {
		slog.Error("error occurred while sending stream", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to stream data to client")

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
func (api *API) setAccession(w http.ResponseWriter, r *http.Request) {
	var accession schema.IngestionAccession
	fileID := r.URL.Query().Get("fileid")
	accessionID := r.URL.Query().Get("accessionid")
	hasQuery := fileID != "" || accessionID != ""
	missingAccession := fileID != "" && accessionID == ""
	hasBody := r.ContentLength > 0
	switch {
	case hasQuery && hasBody:
		writeErrorStatus(w, http.StatusBadRequest, "recieved both query parameters and json payload")

		return
	case missingAccession:
		writeErrorStatus(w, http.StatusBadRequest, "accessionid not provided")

		return
	case fileID != "" && accessionID != "":
		if _, err := uuid.Parse(fileID); err != nil {
			writeErrorStatus(w, http.StatusBadRequest, "provided fileid could not be parsed as valid uuid")

			return
		}

		fileDetails, err := api.db.GetFileDetails(r.Context(), fileID, "verified")
		if err != nil {
			writeErrorStatus(w, http.StatusBadRequest, "file details not found")

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(r.Context(), fileID)
		if err != nil {
			slog.Debug("failed to decrypt checksum from database", "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, "failed to get decrypted checksuom from database")

			return
		}

		accession.AccessionID = accessionID
		accession.User = fileDetails.User
		accession.FilePath = fileDetails.Path
		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	case r.ContentLength > 0:
		if err := json.NewDecoder(r.Body).Decode(accession); err != nil {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

			return
		}

		fileID, err := api.db.GetFileIDByUserPathAndStatus(r.Context(), accession.User, accession.FilePath, "verified")
		if err != nil {
			if fileID == "" {
				writeErrorStatus(w, http.StatusBadRequest, err.Error())
			} else {
				writeErrorStatus(w, http.StatusInternalServerError, err.Error())
			}

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(r.Context(), fileID)
		if err != nil {
			slog.Debug("error when getting decrypted checksums", "err", err)
			writeErrorStatus(w, http.StatusNotFound, "decrypted checksum not found")

			return
		}

		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}
	default:
		writeErrorStatus(w, http.StatusBadRequest, "missing parameter for payload")

		return
	}

	accession.Type = "accession"

	marshaledMsg, _ := json.Marshal(&accession)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Debug("could not validate accession message", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate accession message, reason: %v", err))

		return
	}

	// TODO: Same as previous
	accessionMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "accession", accessionMessage); err != nil {
		slog.Debug("failed to publish accession message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) createDataset(w http.ResponseWriter, r *http.Request) {
	var dataset dataset
	if err := json.NewDecoder(r.Body).Decode(&dataset); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

		return
	}

	if len(dataset.AccessionIDs) == 0 {
		writeErrorStatus(w, http.StatusBadRequest, "at least one accessionID is requiered")

		return
	}

	// Check that the files the accession ids are linked to belong to the user of the dataset
	for _, accessionID := range dataset.AccessionIDs {
		belongsToUser, err := api.db.CheckAccessionIDOwnedByUser(r.Context(), accessionID, dataset.User)
		if err != nil {
			slog.Error("encountered error during database query", "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, "failed to query database")

			return
		}
		if !belongsToUser {
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("accession ID: %s not found or owned by other user", accessionID))

			return
		}
	}

	mapping := schema.DatasetMapping{
		Type:         "mapping",
		AccessionIDs: dataset.AccessionIDs,
		DatasetID:    dataset.DatasetID,
	}
	marshaledMsg, _ := json.Marshal(&mapping)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Debug(err.Error())
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate mappings message, reason: %v", err))

		return
	}

	mappingsMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "mappings", mappingsMessage); err != nil {
		slog.Debug("failed to publish mappings message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) releaseDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := strings.TrimPrefix(r.URL.Query().Get("dataset"), "/")
	ok, err := api.db.CheckIfDatasetExists(r.Context(), datasetID)
	if err != nil {
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}
	if !ok {
		writeErrorStatus(w, http.StatusBadRequest, "dataset not found")

		return
	}

	status, err := api.db.GetDatasetStatus(r.Context(), datasetID)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err.Error())

		return
	}
	if status != "registered" {
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("dataset not registered, status is %s", status))

		return
	}

	datasetMsg := schema.DatasetRelease{
		Type:      "release",
		DatasetID: datasetID,
	}
	marshaledMsg, _ := json.Marshal(&datasetMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Debug("could not validate release message", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate release message, reason: %v", err))

		return
	}

	releaseMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "accession", releaseMessage); err != nil {
		slog.Debug("failed to publish accession message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("fileid")

	if fileID == "" {
		writeErrorStatus(w, http.StatusBadRequest, "file ID is requiered")

		return
	}

	rotateMsg := schema.KeyRotation{
		Type:   "key_rotation",
		FileID: fileID,
	}

	marshaledMsg, err := json.Marshal(&rotateMsg)
	if err != nil {
		slog.Error("could not marshal message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("could not marshal message, reason: %v", err))

		return
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Debug("could not validate rotatekey message", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate rotatekey message, reason: %v", err))

		return
	}

	rotateMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "rotatekey", rotateMessage); err != nil {
		slog.Debug("failed to publish rotatekey message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.URL.Query().Get("dataset")

	if datasetID == "" {
		writeErrorStatus(w, http.StatusBadRequest, "dataset ID is requiered")

		return
	}

	exists, err := api.db.CheckIfDatasetExists(r.Context(), datasetID)
	if err != nil {
		slog.Error("encountered error during database query", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to check dataset existence")

		return
	}
	if !exists {
		slog.Warn("dataset not found", "dataset_id", datasetID)
		writeErrorStatus(w, http.StatusNotFound, "dataset not found")

		return
	}

	files, err := api.db.GetDatasetFileIDs(r.Context(), datasetID)
	if err != nil {
		slog.Error("failed to get dataset files for dataset", "dataset_id", datasetID, "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to get dataset files")

		return
	}

	if len(files) == 0 {
		slog.Warn("no files found", "dataset_id", datasetID)
		writeErrorStatus(w, http.StatusBadRequest, "no files found for dataset")

		return
	}

	for _, fileID := range files {
		rotateMsg := schema.KeyRotation{
			Type:   "key_rotation",
			FileID: fileID,
		}

		marshaledMsg, err := json.Marshal(&rotateMsg)
		if err != nil {
			slog.Error("failed to marshal rotatekey message", "dataset_id", datasetID, "file_id", fileID, "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal rotation message for file %s, reason: %v", fileID, err))

			return
		}

		if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
			slog.Debug("could not validate rotatekey message", "err", err)
			writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not validate rotatekey message, reason: %v", err))

			return
		}

		rotateKeyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(context.Background(), "rotatekey", rotateKeyMessage); err != nil {
			slog.Debug("failed to publish rotatekey message", "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, err.Error())

			return
		}

	}

	slog.Info("rotation messages sent", "nr_files", len(files), "dataset_id", datasetID)
	w.WriteHeader(http.StatusOK)
}

func (api *API) listActiveUsers(w http.ResponseWriter, r *http.Request) {
	users, err := api.db.ListActiveUsers(r.Context())
	if err != nil {
		slog.Debug("failed to list active users", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string][]string{"users": users})
}

func (api *API) listUserFiles(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	username = strings.TrimPrefix(username, "/")
	username = strings.TrimSuffix(username, "/files")

	// parse optional pagination params
	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err.Error())

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	files, nextCursor, err := api.db.GetUserFiles(r.Context(), username, pathPrefix, true, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			writeErrorStatus(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	if nextCursor != "" {
		w.Header().Set("X-Next-Cursor", nextCursor)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rsp)
}

// addC4ghHash handles the addition of a hashed public key to the database.
// It expects a JSON payload containing the base64 encoded public key and its description.
// If the JSON payload is invalid, it responds with a 400 Bad Request status.
// If the hash is already in the database, it responds with a 409 Conflict status
// If the database insertion fails, it responds with a 500 Internal Server Error status.
// On success, it responds with a 200 OK status.
func (api *API) addC4ghHash(w http.ResponseWriter, r *http.Request) {
	var c4gh schema.C4ghPubKey
	if err := json.NewDecoder(r.Body); err != nil {
		slog.Error("could not base64 decode public key", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

		return
	}

	b64d, err := base64.StdEncoding.DecodeString(c4gh.PubKey)
	if err != nil {
		slog.Error("could not base64 decode public key", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not decode public key, reason: %v", err))

		return
	}

	pubKey, err := keys.ReadPublicKey(bytes.NewReader(b64d))
	if err != nil {
		slog.Error("could not read public key", "base64_encoding", b64d, "err", err)
		writeErrorStatus(w, http.StatusBadRequest, fmt.Sprintf("could not read public key, reason: %v", err))

		return
	}

	keyHash := hex.EncodeToString(pubKey[:])
	err = api.db.AddKeyHash(r.Context(), keyHash, c4gh.Description)
	if err != nil {
		if strings.Contains(err.Error(), "key hash already exists") {
			slog.Error("key hash already exists", "key_hash", keyHash, "err", err)
			writeErrorStatus(w, http.StatusBadRequest, "key hash already exists")
		} else {
			slog.Error("failed to insert key hash to database", "key_hash", keyHash, "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, "failed to insert key hash to database")
		}

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) listC4ghHashes(w http.ResponseWriter, r *http.Request) {
	hashes, err := api.db.ListKeyHashes(r.Context())
	if err != nil {
		slog.Error("failed to list c4gh key hashes", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to list c4gh key hashes")

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
	w.Header().Set("Content-Type", "application/json")

	rsp := make([]*c4ghKeyHash, len(hashes))

	for i, hash := range hashes {
		rsp[i] = &c4ghKeyHash{
			Hash:         hash.Hash,
			Description:  hash.Description,
			CreatedAt:    hash.CreatedAt,
			DeprecatedAt: hash.DeprecatedAt,
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) deprecateC4ghHash(w http.ResponseWriter, r *http.Request) {
	keyHash := strings.TrimPrefix(r.URL.Query().Get("keyHash"), "/")
	err := api.db.DeprecateKeyHash(r.Context(), keyHash)
	if err != nil {
		slog.Error("failed to deprecate key hash", "err", err)
		writeErrorStatus(w, http.StatusBadRequest, "failed to deprecate key hash")

		return
	}
}

func (api *API) listAllDatasets(w http.ResponseWriter, r *http.Request) {
	datasets, err := api.db.ListDatasets(r.Context())
	if err != nil {
		slog.Error("failed to list datasets", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to list datasets")

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

	w.WriteHeader(http.StatusOK)
}

func (api *API) listUserDatasets(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Query().Get("username"), "/")
	datasets, err := api.db.ListUserDatasets(r.Context(), username)
	if err != nil {
		slog.Error("failed to list users datasets", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "failed to list users datasets")

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

	w.WriteHeader(http.StatusOK)
}

func (api *API) listDatasets(w http.ResponseWriter, r *http.Request) {
	token, err := api.auth.Authenticate(r)
	if err != nil {
		slog.Error("could not authenticate user", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "could not authenticate user")

		return
	}

	datasets, err := api.db.ListUserDatasets(r.Context(), token.Subject())
	if err != nil {
		slog.Error("could not list users datasets", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "could not list users datasets")

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rsp)
}

func (api *API) reVerifyFile(w http.ResponseWriter, r *http.Request) {
	accessionID := strings.TrimPrefix(r.URL.Query().Get("accession"), "/")
	reverificationData, err := api.db.GetReVerificationData(r.Context(), accessionID)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			slog.Error("accession ID not found", "err", err)
			writeErrorStatus(w, http.StatusNotFound, "accession ID not found")

			return
		} else {
			slog.Error("could not retrieve reverification data", "err", err)
			writeErrorStatus(w, http.StatusNotFound, "could not retrieve reverification data")

			return
		}
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
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate reverify message", "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, "could not validate reverify message")

		return
	}

	reVerifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "archived", reVerifyMessage); err != nil {
		writeErrorStatus(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) reVerifyDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := strings.TrimPrefix(r.URL.Query().Get("dataset"), "/")
	files, err := api.db.GetDatasetFiles(r.Context(), datasetID)
	if err != nil {
		slog.Error("could not get files for dataset", "dataset_id", datasetID, "err", err)
		writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("could not get files for dataset: %s", datasetID))

		return
	}

	if files == nil {
		slog.Error("no files found for dataset", "dataset_id", datasetID, "err", err)
		writeErrorStatus(w, http.StatusNotFound, fmt.Sprintf("no files found for dataset %s", datasetID))

		return
	}

	for _, accessionID := range files {
		reverificationData, err := api.db.GetReVerificationData(r.Context(), accessionID)
		if err != nil {
			slog.Error("could not get reverification data from database", "accession_id", accessionID, "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("could not get reverification data from database"))

			return
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
		if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
			slog.Error("could not validate reverify message", "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, "could not validate reverify message")

			return
		}

		reVerifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(context.Background(), "archived", reVerifyMessage); err != nil {
			slog.Debug("failed to publish reverify message", "err", err)
			writeErrorStatus(w, http.StatusInternalServerError, err.Error())

			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
