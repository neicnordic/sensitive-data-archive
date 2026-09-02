package main

import (
	"bytes"
	"context"
	"database/sql"
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	apiconfig "github.com/neicnordic/sensitive-data-archive/cmd/api/config"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
)

func (api *API) rbac(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := api.auth.Authenticate(r)
		if err != nil {
			slog.Error("failed to authorize request", "err", err)
			writeJSON(w, http.StatusUnauthorized, "failed to authorize request")

			return
		}

		subject := token.Subject()
		urlPath := r.URL.Path
		method := r.Method

		ok, err := api.enforcer.Enforce(subject, urlPath, method)
		if err != nil {
			// #nosec G706 -- slog safely escapes structured attributes natively
			slog.Error("failed to enforce subject for the request url", "subject", subject, "method", method, "url_path", urlPath, "err", err)
			writeJSON(w, http.StatusInternalServerError, "failed to enforce subject for the requested url")

			return
		}

		if !ok {
			// #nosec G706 -- slog safely escapes structured attributes natively
			slog.Warn("unathorized request", "subject", subject, "method", method, "url_path", urlPath)
			writeJSON(w, http.StatusUnauthorized, "not authorized")

			return
		}

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

func (api *API) readinessResponse(w http.ResponseWriter, r *http.Request) {
	if !api.mq.Alive() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unable to reach rabbitmq"))

		return
	}

	if err := api.db.Ping(context.Background()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unable to reach database"))

		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
	token, err := api.auth.Authenticate(r)
	if err != nil {
		slog.Error("failed to authenticate user", "err", err)
		writeJSON(w, http.StatusUnauthorized, "failed to authenticate user")

		return
	}

	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		slog.Error("failed to parse pagination limit", "err", err)
		writeJSON(w, http.StatusBadRequest, err.Error())

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	files, nextCursor, err := api.db.GetUserFiles(r.Context(), token.Subject(), pathPrefix, false, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			// #nosec G706 -- slog safely escapes structured attributes natively
			slog.Error("invalid cursor parameter", "cursor", cursor, "limig", limit, "path_prefix", pathPrefix, "err", err)
			writeJSON(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		writeJSON(w, http.StatusInternalServerError, err.Error())

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
	_ = json.NewEncoder(w).Encode(rsp)
}

func (api *API) updateFileEvent(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileid")
	event := r.PathValue("event")

	token, err := api.auth.Authenticate(r)
	if err != nil {
		slog.Error("could not authenticate request", "err", err)
		writeJSON(w, http.StatusUnauthorized, "could not authenticate request")

		return
	}

	type updateFileEventBody struct {
		Reason string `json:"reason"`
	}
	updateEvent := &updateFileEventBody{}
	if err := json.NewDecoder(r.Body).Decode(&updateEvent); err != nil {
		slog.Error("could not decode request body", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

		return
	}

	if updateEvent.Reason == "" {
		writeJSON(w, http.StatusBadRequest, "no reason found in request body")

		return
	}

	details, err := json.Marshal(updateEvent)
	if err != nil {
		slog.Error("failed to marshal updateEvent", "err", err)
		writeJSON(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal JSON encoding, reason: %v", err))

		return
	}

	fileEvents, err := api.db.GetFileEvents(r.Context())
	if err != nil {
		slog.Error("failed to get file events", "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to get file events")

		return
	}

	if !slices.Contains(fileEvents, event) {
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf(
			"event %q not allowed, needs one of: %s",
			event, strings.Join(fileEvents, ", "),
		))

		return
	}

	err = api.db.UpdateFileEventLog(r.Context(), fileID, event, token.Subject(), string(details), "{}")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Error("file not found", "file", fileID) // #nosec G706
			writeJSON(w, http.StatusNotFound, fmt.Sprintf("file %q not found", fileID))

			return
		}
		slog.Error("failed to update file event log", "file_id", fileID, "event", event, "err", err) // #nosec G706
		writeJSON(w, http.StatusInternalServerError, "failed to update file event")

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) getFileEvents(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileid")

	statusHistory, err := api.db.GetFileStatusHistory(r.Context(), fileID)
	if err != nil {
		slog.Error("failed to get file status history", "file_id", fileID, "err", err) // #nosec G706
		writeJSON(w, http.StatusInternalServerError, "failed to get file status history")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]database.FileStatus{"events": statusHistory})
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
		ingest schema.IngestionTrigger
		err    error
	)

	fileID := r.URL.Query().Get("fileid")

	switch {
	case fileID != "" && r.ContentLength > 0:
		slog.Error("recieved both file ID and payload")
		writeJSON(w, http.StatusBadRequest, "recieved both file ID and payload")

		return

	case fileID != "":
		if _, err := uuid.Parse(fileID); err != nil {
			slog.Error("could not parse fileID as uuid", "file_id", fileID, "err", err) // #nosec G706
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not parse %s as uuid, reason: %v", fileID, err))

			return
		}

		fileDetails, err := api.db.GetFileDetails(r.Context(), fileID, "uploaded")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not find details for %s, reason: %v", fileID, err))

			return
		}

		ingest.User = fileDetails.User
		ingest.FilePath = fileDetails.Path

	case r.ContentLength > 0:
		if err := json.NewDecoder(r.Body).Decode(&ingest); err != nil {
			slog.Error("could not decode request body", "err", err)
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

			return
		}

		fileID, err = api.db.GetFileIDByUserPathAndStatus(context.TODO(), ingest.User, ingest.FilePath, "uploaded")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, err.Error())

			return
		}

		if fileID == "" {
			slog.Error("could not find fileID for user and filepath", "submission_user", ingest.User, "file_path", ingest.FilePath) // #nosec G706
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not find fileID for %s", ingest.FilePath))

			return
		}

	default:
		slog.Error("missing parameter in payload")
		writeJSON(w, http.StatusBadRequest, "missing parameter in payload")

		return
	}

	slog.Info("ingesting file", "file_id", fileID) // #nosec G706
	ingest.Type = "ingest"
	marshaledMsg, _ := json.Marshal(&ingest)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate ingestion message", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate ingestion message, reason: %v", err))

		return
	}

	ingestMessage := broker.Message{Key: fileID, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "ingest", ingestMessage); err != nil {
		slog.Debug("failed to publish ingest message", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	fileID := r.PathValue("fileid")
	// #nosec G706 -- slog safely escapes structured attributes natively
	slog.Info("handling request to delete file", "username", username, "file_id", fileID)
	if fileID == "" {
		slog.Error("file ID is requiered")
		writeJSON(w, http.StatusBadRequest, "file ID is requiered")

		return
	}

	// Get the file path from the fileID and submission user
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(r.Context(), username, fileID)
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("file could not be found in inbox", "file_id", fileID, "err", err)
		writeJSON(w, http.StatusNotFound, "file could not be found in inbox")

		return
	}

	if location == "" {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("no known submission location found", "file_id", fileID)
		writeJSON(w, http.StatusInternalServerError, "failed to find file in location")

		return
	}

	filePath = helper.UnanonymizeFilepath(filePath, username)
	for count := 1; count <= 5; count++ {
		err = api.inboxWriter.RemoveFile(r.Context(), location, filePath)
		if err == nil {
			break
		}

		slog.Error("failed to remove file from inbox", "err", err)
		if count == 5 {
			writeJSON(w, http.StatusInternalServerError, "failed to remove file from inbox")

			return
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	if err := api.db.UpdateFileEventLog(r.Context(), fileID, "disabled", "api", "{}", "{}"); err != nil {
		slog.Error("set status deleted failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

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
	c := reencrypt.NewReencryptClient(api.grpcClient)
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
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("could not decode c4gh public key", "public_key", pubKey, "err", err)
		writeJSON(w, http.StatusBadRequest, "bad public key")

		return
	}

	fileID := r.PathValue("fileid")
	submissionUser := r.PathValue("username")
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(r.Context(), submissionUser, fileID)
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("failed to get filepath from fileID", "file_id", fileID, "err", err)
		writeJSON(w, http.StatusNotFound, "failed to retrieve inbox file path")

		return
	}
	if location == "" {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("fileID has no known submission location", "file_id", fileID, "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to find file location")

		return
	}

	file, err := api.inboxReader.NewFileReader(r.Context(), location, helper.UnanonymizeFilepath(filePath, submissionUser))
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("inbox file not found or failed to read", "file_path", filePath, "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to read inbox file")

		return
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := headers.ReadHeader(file)
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("failed to read file header", "file_id", fileID, "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to read file header")

		return
	}

	newHeader, err := api.reencryptHeader(r.Context(), header, c4ghPubKey)
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("failed to reencrypt header", "file_id", fileID, "c4gh_public_key", c4ghPubKey, "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to reencrypt header")

		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", path.Base(filePath)))

	reader := io.MultiReader(bytes.NewReader(newHeader), file)
	_, err = io.Copy(w, reader)
	if err != nil {
		// #nosec G706 -- slog safely escapes structured attributes natively
		slog.Error("error occurred while sending stream", "file_id", fileID, "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to stream data to client")

		return
	}
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
	var fileDetails *database.FileDetails
	var err error

	fileID := r.URL.Query().Get("fileid")
	accessionID := r.URL.Query().Get("accessionid")
	hasQuery := fileID != "" || accessionID != ""
	hasBody := r.ContentLength > 0

	switch {
	case hasQuery:
		if _, err := uuid.Parse(fileID); err != nil {
			slog.Error("provided fileid could not be parsed as valid uuid", "file_id", fileID, "err", err) // #nosec G706
			writeJSON(w, http.StatusBadRequest, "provided fileid could not be parsed as valid uuid")

			return
		}

		fileDetails, err = api.db.GetFileDetails(r.Context(), fileID, "verified")
		if err != nil {
			slog.Error("file details not found", "file_id", fileID, "err", err) // #nosec G706
			writeJSON(w, http.StatusBadRequest, "file details not found")

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(r.Context(), fileID)
		if err != nil {
			slog.Debug("failed to decrypt checksum from database", "err", err)
			writeJSON(w, http.StatusInternalServerError, "failed to get decrypted checksum from database")

			return
		}

		accession.AccessionID = accessionID
		accession.User = fileDetails.User
		accession.FilePath = fileDetails.Path
		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	case hasBody:
		if err := json.NewDecoder(r.Body).Decode(&accession); err != nil {
			slog.Error("could not decode request body", "err", err)
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

			return
		}

		var err error
		fileID, err = api.db.GetFileIDByUserPathAndStatus(r.Context(), accession.User, accession.FilePath, "verified")
		if err != nil {
			if fileID == "" {
				slog.Error("failed to get fileid for user", "user", accession.User, "file_path", accession.FilePath, "err", err) // #nosec G706
				writeJSON(w, http.StatusBadRequest, err.Error())
			} else {
				slog.Error("unexpected error occurred", "err", err)
				writeJSON(w, http.StatusInternalServerError, err.Error())
			}

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(r.Context(), fileID)
		if err != nil {
			slog.Error("error when getting decrypted checksums", "err", err)
			writeJSON(w, http.StatusNotFound, "decrypted checksum not found")

			return
		}

		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	default:
		slog.Error("requiered parameters not found")
		writeJSON(w, http.StatusBadRequest, "requiered parameters not found, need either query parameters (fileid, accessionid) or httpbody (filepath & accession_id)")

		return
	}

	accession.Type = "accession"
	marshaledMsg, _ := json.Marshal(&accession)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate accession message", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate accession message, reason: %v", err))

		return
	}

	slog.Info("sending accession message", "file_id", fileID, "accession_id", accession.AccessionID) // #nosec G706
	if err := api.mq.Publish(context.Background(), "accession", broker.Message{Key: fileID, Body: marshaledMsg}); err != nil {
		slog.Debug("failed to publish accession message", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) getDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("datasetid")

	if datasetID == "" {
		slog.Error("missing dataset id in request")
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: http.StatusText(http.StatusBadRequest),
		})

		return
	}

	datasetDetails, err := api.db.GetDatasetDetails(r.Context(), datasetID)
	if err != nil {
		slog.Error("failed to get dataset details", "error", err, "dataset_id", datasetID)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: http.StatusText(http.StatusInternalServerError),
		})

		return
	}
	if datasetDetails == nil {
		slog.Error("dataset not found", "dataset_id", datasetID)
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error: http.StatusText(http.StatusNotFound),
		})

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&struct {
		Status        string `json:"status"`
		CreatedAt     string `json:"createdAt"`
		NumberOfFiles uint64 `json:"numberOfFiles"`
	}{
		Status:        datasetDetails.Status,
		CreatedAt:     datasetDetails.CreatedAt,
		NumberOfFiles: datasetDetails.NumberOfFiles,
	})
}
func (api *API) createDataset(w http.ResponseWriter, r *http.Request) {
	var dataset dataset
	if err := json.NewDecoder(r.Body).Decode(&dataset); err != nil {
		slog.Error("could not decode request body", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

		return
	}

	if len(dataset.AccessionIDs) == 0 {
		slog.Error("no accession IDs recieved")
		writeJSON(w, http.StatusBadRequest, "no accession IDs recieved")

		return
	}

	// Validate that the files to have overridden download paths are added to the dataset in the same request
	for accessionToSetDownloadPath, downloadPath := range dataset.FileDownloadPaths {
		if downloadPath == "" {
			writeJSON(w, http.StatusBadRequest, "download path for a file can not be empty")

			return
		}
		found := false
		for _, accessionToAddToDataset := range dataset.AccessionIDs {
			if accessionToSetDownloadPath == accessionToAddToDataset {
				found = true

				break
			}
		}
		if !found {
			slog.Error("attempted to set file download path for a file not being added to the dataset")
			writeJSON(w, http.StatusBadRequest, "attempted to set file download path for a file not being added to the dataset")

			return
		}
	}

	// Check that the files the accession ids are linked to belong to the user of the dataset
	for _, accessionID := range dataset.AccessionIDs {
		md, err := api.db.GetMappingData(r.Context(), accessionID)
		if err != nil {
			slog.Error("encountered error during database query", "err", err)
			writeJSON(w, http.StatusInternalServerError, "failed to query database")

			return
		}
		if md == nil {
			slog.Info("rejecting create dataset request including non-existing id", "accession_id", accessionID)
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("no file exists with accession: %s", accessionID))

			return
		}
	}

	mapping := schema.DatasetMapping{
		Type:              "mapping",
		AccessionIDs:      dataset.AccessionIDs,
		DatasetID:         dataset.DatasetID,
		FileDownloadPaths: dataset.FileDownloadPaths,
	}
	marshaledMsg, _ := json.Marshal(&mapping)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-mapping.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Debug(err.Error())
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate mappings message, reason: %v", err))

		return
	}

	mappingsMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "mappings", mappingsMessage); err != nil {
		slog.Debug("failed to publish mappings message", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) releaseDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("datasetid")
	ok, err := api.db.CheckIfDatasetExists(r.Context(), datasetID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}
	if !ok {
		slog.Error("dataset not found", "dataset_id", datasetID) // #nosec G706
		writeJSON(w, http.StatusBadRequest, "dataset not found")

		return
	}

	status, err := api.db.GetDatasetStatus(r.Context(), datasetID)
	if err != nil {
		slog.Error("failed to get dataset status", "dataset_id", datasetID, "err", err) // #nosec G706
		writeJSON(w, http.StatusBadRequest, err.Error())

		return
	}
	if status != "registered" {
		slog.Error("dataset not registered", "dataset_id", datasetID, "status", status) // #nosec G706
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("dataset not registered, status is %s", status))

		return
	}

	datasetMsg := schema.DatasetRelease{
		Type:      "release",
		DatasetID: datasetID,
	}
	marshaledMsg, _ := json.Marshal(&datasetMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate release message", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate release message, reason: %v", err))

		return
	}

	releaseMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "mappings", releaseMessage); err != nil {
		slog.Debug("failed to publish dataset release message", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileid")

	if fileID == "" {
		slog.Error("file ID missing")
		writeJSON(w, http.StatusBadRequest, "file ID missing")

		return
	}

	rotateMsg := schema.KeyRotation{
		Type:   "key_rotation",
		FileID: fileID,
	}

	marshaledMsg, err := json.Marshal(&rotateMsg)
	if err != nil {
		slog.Error("could not marshal message", "err", err)
		writeJSON(w, http.StatusInternalServerError, fmt.Sprintf("could not marshal message, reason: %v", err))

		return
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate rotatekey message", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate rotatekey message, reason: %v", err))

		return
	}

	rotateMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "rotatekey", rotateMessage); err != nil {
		slog.Debug("failed to publish rotatekey message", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("datasetid")
	if datasetID == "" {
		slog.Error("no dataset id found", "dataset_id", datasetID) // #nosec G706
		writeJSON(w, http.StatusBadRequest, "no dataset id found")

		return
	}

	exists, err := api.db.CheckIfDatasetExists(r.Context(), datasetID)
	if err != nil {
		slog.Error("encountered error during database query", "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to check dataset existence")

		return
	}
	if !exists {
		slog.Warn("dataset not found", "dataset_id", datasetID) // #nosec G706
		writeJSON(w, http.StatusNotFound, "dataset not found")

		return
	}

	files, err := api.db.GetDatasetFileIDs(r.Context(), datasetID)
	if err != nil {
		slog.Error("failed to get dataset files for dataset", "dataset_id", datasetID, "err", err) // #nosec G706
		writeJSON(w, http.StatusInternalServerError, "failed to get dataset files")

		return
	}

	if len(files) == 0 {
		slog.Warn("no files found", "dataset_id", datasetID) // #nosec G706
		writeJSON(w, http.StatusBadRequest, "no files found for dataset")

		return
	}
	for _, fileID := range files {
		rotateMsg := schema.KeyRotation{
			Type:   "key_rotation",
			FileID: fileID,
		}
		marshaledMsg, err := json.Marshal(&rotateMsg)
		if err != nil {
			slog.Error("failed to marshal rotatekey message", "dataset_id", datasetID, "file_id", fileID, "err", err) // #nosec G706
			writeJSON(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal rotation message for file %s, reason: %v", fileID, err))

			return
		}

		if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
			slog.Error("could not validate rotatekey message", "err", err)
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not validate rotatekey message, reason: %v", err))

			return
		}

		rotateKeyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(context.Background(), "rotatekey", rotateKeyMessage); err != nil {
			slog.Error("failed to publish rotatekey message", "err", err)
			writeJSON(w, http.StatusInternalServerError, err.Error())

			return
		}
	}

	slog.Info("rotation messages sent", "nr_files", len(files), "dataset_id", datasetID) // #nosec G706
	w.WriteHeader(http.StatusOK)
}

func (api *API) listActiveUsers(w http.ResponseWriter, r *http.Request) {
	users, err := api.db.ListActiveUsers(r.Context())
	if err != nil {
		slog.Debug("failed to list active users", "err", err)
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]string{"users": users})
}

func (api *API) listUserFiles(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	// parse optional pagination params
	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error())

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	slog.Info("getting files", "username", username, "pathPrefix", pathPrefix, "limit", limit, "cursor", cursor) // #nosec G706
	files, nextCursor, err := api.db.GetUserFiles(r.Context(), username, pathPrefix, true, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			writeJSON(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		writeJSON(w, http.StatusInternalServerError, err.Error())

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
	_ = json.NewEncoder(w).Encode(rsp)
}

// addC4ghHash handles the addition of a hashed public key to the database.
// It expects a JSON payload containing the base64 encoded public key and its description.
// If the JSON payload is invalid, it responds with a 400 Bad Request status.
// If the hash is already in the database, it responds with a 409 Conflict status
// If the database insertion fails, it responds with a 500 Internal Server Error status.
// On success, it responds with a 200 OK status.
func (api *API) addC4ghHash(w http.ResponseWriter, r *http.Request) {
	var c4gh schema.C4ghPubKey
	if err := json.NewDecoder(r.Body).Decode(&c4gh); err != nil {
		slog.Error("could not decode request body", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode request body, reason: %v", err))

		return
	}

	b64d, err := base64.StdEncoding.DecodeString(c4gh.PubKey)
	if err != nil {
		slog.Error("could not base64 decode public key", "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not decode public key, reason: %v", err))

		return
	}

	pubKey, err := keys.ReadPublicKey(bytes.NewReader(b64d))
	if err != nil {
		slog.Error("could not read public key", "base64_encoding", b64d, "err", err)
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not read public key, reason: %v", err))

		return
	}

	keyHash := hex.EncodeToString(pubKey[:])
	err = api.db.AddKeyHash(r.Context(), keyHash, c4gh.Description)
	if err != nil {
		if strings.Contains(err.Error(), "key hash already exists") {
			slog.Error("key hash already exists", "key_hash", keyHash, "err", err)
			writeJSON(w, http.StatusConflict, "key hash already exists")
		} else {
			slog.Error("failed to insert key hash to database", "key_hash", keyHash, "err", err)
			writeJSON(w, http.StatusInternalServerError, "failed to insert key hash to database")
		}

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) listC4ghHashes(w http.ResponseWriter, r *http.Request) {
	hashes, err := api.db.ListKeyHashes(r.Context())
	if err != nil {
		slog.Error("failed to list c4gh key hashes", "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to list c4gh key hashes")

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rsp)
}

func (api *API) deprecateC4ghHash(w http.ResponseWriter, r *http.Request) {
	keyHash := r.PathValue("keyhash")
	err := api.db.DeprecateKeyHash(r.Context(), keyHash)
	if err != nil {
		slog.Error("failed to deprecate key hash", "key_hash", keyHash, "err", err) // #nosec G706
		writeJSON(w, http.StatusBadRequest, "failed to deprecate key hash")

		return
	}
}

func (api *API) listAllDatasets(w http.ResponseWriter, r *http.Request) {
	datasets, err := api.db.ListDatasets(r.Context())
	if err != nil {
		slog.Error("failed to list datasets", "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to list datasets")

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
	_ = json.NewEncoder(w).Encode(rsp)
}

func (api *API) listUserDatasets(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	datasets, err := api.db.ListUserDatasets(r.Context(), username)
	if err != nil {
		slog.Error("failed to list users datasets", "err", err)
		writeJSON(w, http.StatusInternalServerError, "failed to list users datasets")

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
	_ = json.NewEncoder(w).Encode(rsp)
}

func (api *API) listDatasets(w http.ResponseWriter, r *http.Request) {
	token, err := api.auth.Authenticate(r)
	if err != nil {
		slog.Error("could not authenticate user")
		writeJSON(w, http.StatusInternalServerError, "could not authenticate user")

		return
	}

	datasets, err := api.db.ListUserDatasets(r.Context(), token.Subject())
	if err != nil {
		slog.Error("could not list users datasets", "err", err)
		writeJSON(w, http.StatusInternalServerError, "could not list users datasets")

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
	_ = json.NewEncoder(w).Encode(rsp)
}

func (api *API) verifyFile(w http.ResponseWriter, r *http.Request) {
	accessionID := r.PathValue("accession")
	reverificationData, err := api.db.GetReVerificationData(r.Context(), accessionID)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			slog.Error("accession ID not found", "err", err)
			writeJSON(w, http.StatusNotFound, "accession ID not found")

			return
		}
		slog.Error("could not retrieve reverification data", "err", err)
		writeJSON(w, http.StatusNotFound, "could not retrieve reverification data")

		return
	}

	verifyMsg := schema.IngestionVerification{
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
	marshaledMsg, _ := json.Marshal(&verifyMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		slog.Error("could not validate verification message", "err", err)
		writeJSON(w, http.StatusInternalServerError, "could not validate verification message")

		return
	}

	verifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(context.Background(), "archived", verifyMessage); err != nil {
		writeJSON(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) verifyDataset(w http.ResponseWriter, r *http.Request) {
	datasetID := r.PathValue("dataset")
	files, err := api.db.GetDatasetFiles(r.Context(), datasetID)
	if err != nil {
		slog.Error("could not get files for dataset", "dataset_id", datasetID, "err", err) // #nosec G706
		writeJSON(w, http.StatusInternalServerError, fmt.Sprintf("could not get files for dataset: %s", datasetID))

		return
	}

	if files == nil {
		slog.Error("no files found for dataset", "dataset_id", datasetID) // #nosec G706
		writeJSON(w, http.StatusNotFound, fmt.Sprintf("no files found for dataset %s", datasetID))

		return
	}

	for _, accessionID := range files {
		reverificationData, err := api.db.GetReVerificationData(r.Context(), accessionID)
		if err != nil {
			slog.Error("could not get reverification data from database", "accession_id", accessionID, "err", err) // #nosec G706
			writeJSON(w, http.StatusInternalServerError, "could not get reverification data from database")

			return
		}
		verifyMsg := schema.IngestionVerification{
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
		marshaledMsg, _ := json.Marshal(&verifyMsg)
		if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
			slog.Error("could not validate verification message", "err", err)
			writeJSON(w, http.StatusInternalServerError, "could not validate verification message")

			return
		}

		verifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(context.Background(), "archived", verifyMessage); err != nil {
			slog.Debug("failed to publish verification message", "err", err)
			writeJSON(w, http.StatusInternalServerError, err.Error())

			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
