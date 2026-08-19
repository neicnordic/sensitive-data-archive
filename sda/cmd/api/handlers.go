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
	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
)

func (api *API) rbac(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := observability.StartSpan(r.Context(), "rbac")
		defer span.End()
		r = r.WithContext(ctx)

		token, err := api.auth.Authenticate(r)
		if err != nil {
			span.Error("failed to authorize request", err)
			writeJSON(w, http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))

			return
		}

		subject := token.Subject()
		urlPath := r.URL.Path
		method := r.Method

		ok, err := api.enforcer.Enforce(subject, urlPath, method)
		if err != nil {
			span.Error("failed to enforce subject for the request url", err,
				slog.String("subject", subject),
				slog.String("method", method),
				slog.String("url_path", urlPath),
			)
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		if !ok {
			span.Warn("unauthorized request",
				slog.String("subject", subject),
				slog.String("method", method),
				slog.String("url_path", urlPath),
			)
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
				slog.Error("panic recovered", "error", err)
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

	if err := api.db.Ping(r.Context()); err != nil {
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
	ctx, span := observability.StartSpan(r.Context(), "getFiles")
	defer span.End()

	token, err := api.auth.Authenticate(r)
	if err != nil {
		// This is internal error as rbac middleware should have authenticated it already
		span.Error("failed to authenticate user", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		span.Warn("failed to parse pagination limit")
		writeJSON(w, http.StatusBadRequest, "invalid limit")

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	files, nextCursor, err := api.db.GetUserFiles(ctx, token.Subject(), pathPrefix, false, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			span.Error("invalid cursor parameter", err,
				slog.String("cursor", cursor),
				slog.Int("limit", limit),
				slog.String("path_prefix", pathPrefix),
			)
			writeJSON(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "updateFileEvent")
	defer span.End()

	fileID := r.PathValue("fileid")
	event := r.PathValue("event")

	token, err := api.auth.Authenticate(r)
	if err != nil {
		// This is internal error as rbac middleware should have authenticated it already
		span.Error("failed to authenticate user", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	type updateFileEventBody struct {
		Reason string `json:"reason"`
	}
	updateEvent := &updateFileEventBody{}
	if err := json.NewDecoder(r.Body).Decode(&updateEvent); err != nil {
		span.Warn("could not decode request body", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, http.StatusText(http.StatusBadRequest))

		return
	}

	if updateEvent.Reason == "" {
		writeJSON(w, http.StatusBadRequest, "no reason found in request body")

		return
	}

	details, err := json.Marshal(updateEvent)
	if err != nil {
		span.Error("failed to marshal updateEvent", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	fileEvents, err := api.db.GetFileEvents(ctx)
	if err != nil {
		span.Error("failed to get file events", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if !slices.Contains(fileEvents, event) {
		writeJSON(w, http.StatusBadRequest, fmt.Sprintf(
			"event %q not allowed, needs one of: %s",
			event, strings.Join(fileEvents, ", "),
		))

		return
	}

	if err := api.db.UpdateFileEventLog(ctx, fileID, event, token.Subject(), string(details), "{}"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Error("file not found", err, slog.String("file", fileID))
			writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		}
		span.Error("failed to update file event log", err, slog.String("file_id", fileID), slog.String("event", event))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) getFileEvents(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "getFileEvents")
	defer span.End()

	fileID := r.PathValue("fileid")

	statusHistory, err := api.db.GetFileStatusHistory(ctx, fileID)
	if err != nil {
		span.Error("failed to get file status history", err, slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "ingestFile")
	defer span.End()

	var (
		ingest schema.IngestionTrigger
		err    error
	)

	fileID := r.URL.Query().Get("fileid")

	switch {
	case fileID != "" && r.ContentLength > 0:
		span.Warn("received both file ID and payload")
		writeJSON(w, http.StatusBadRequest, "received both file ID and payload")

		return

	case fileID != "":
		if _, err := uuid.Parse(fileID); err != nil {
			span.Warn("could not parse fileID as uuid", slog.String("file_id", fileID))
			writeJSON(w, http.StatusBadRequest, fmt.Sprintf("could not parse %s as uuid, reason: %v", fileID, err))

			return
		}

		fileDetails, err := api.db.GetFileDetails(ctx, fileID, "uploaded")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				span.Warn("file details not found", slog.String("file_id", fileID))
				writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

				return
			}
			span.Error("failed to get file details", err, slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		ingest.User = fileDetails.User
		ingest.FilePath = fileDetails.Path

	case r.ContentLength > 0:
		if err := json.NewDecoder(r.Body).Decode(&ingest); err != nil {
			span.Warn("could not decode request body", slog.Any("error", err))
			writeJSON(w, http.StatusBadRequest, "invalid request body")

			return
		}

		fileID, err = api.db.GetFileIDByUserPathAndStatus(ctx, ingest.User, ingest.FilePath, "uploaded")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				span.Warn("uploaded file not found", slog.String("user", ingest.User), slog.String("file_path", ingest.FilePath))
				writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

				return
			}

			span.Error("failed to get file id by user, path, and status", err, slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

	default:
		span.Warn("missing parameter in payload")
		writeJSON(w, http.StatusBadRequest, "missing parameter in payload")

		return
	}

	span.Info("ingesting file", slog.String("file_id", fileID))
	ingest.Type = "ingest"
	marshaledMsg, _ := json.Marshal(&ingest)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		span.Error("could not validate ingestion message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	ingestMessage := broker.Message{Key: fileID, Body: marshaledMsg}
	if err := api.mq.Publish(ctx, "ingest", ingestMessage); err != nil {
		span.Error("failed to publish ingest message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "deleteFile")
	defer span.End()

	username := r.PathValue("username")
	fileID := r.PathValue("fileid")

	if _, err := uuid.Parse(fileID); err != nil {
		span.Warn("invalid file ID", slog.Any("error", err), slog.String("file_id", fileID))
		writeJSON(w, http.StatusBadRequest, "invalid file id")

		return
	}

	// Get the file path from the fileID and submission user
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(ctx, username, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Warn("file not found", slog.String("file_id", fileID), slog.String("user", username))
			writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		}
		span.Error("failed to get file submission path", err, slog.String("file_id", fileID), slog.String("user", username))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if location == "" {
		span.Error("no known submission location found", errors.New("file has no submission location"), slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	filePath = helper.UnanonymizeFilepath(filePath, username)

	if err := api.inboxWriter.RemoveFile(ctx, location, filePath); err != nil {
		span.Error("failed to remove file from inbox", err, slog.String("location", location), slog.String("file_path", filePath))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if err := api.db.UpdateFileEventLog(ctx, fileID, "disabled", "api", "{}", "{}"); err != nil {
		span.Error("failed to set file status disabled", err, slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(ctx, "reencryptHeader")
	defer span.End()

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
	ctx, span := observability.StartSpan(r.Context(), "downloadFile")
	defer span.End()

	c4ghPubKey := r.Header.Get("C4GH-Public-Key")

	pubKey, err := base64.StdEncoding.DecodeString(c4ghPubKey)
	if err != nil || len(pubKey) == 0 {
		span.Warn("could not decode c4gh callers public key")
		writeJSON(w, http.StatusBadRequest, "bad public key")

		return
	}

	fileID := r.PathValue("fileid")
	if _, err := uuid.Parse(fileID); err != nil {
		span.Warn("invalid file ID", slog.String("file-id", fileID))
		writeJSON(w, http.StatusBadRequest, "invalid file id")

		return
	}

	submissionUser := r.PathValue("username")
	filePath, location, err := api.db.GetUploadedSubmissionFilePathAndLocation(ctx, submissionUser, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Warn("file not found", slog.String("file_id", fileID), slog.String("user", submissionUser))
			writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		}
		span.Error("failed to get file submission path", err, slog.String("file_id", fileID), slog.String("user", submissionUser))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
	if location == "" {
		span.Error("fileID has no known submission location", errors.New("file has no submission location"), slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	file, err := api.inboxReader.NewFileReader(ctx, location, helper.UnanonymizeFilepath(filePath, submissionUser))
	if err != nil {
		span.Error("inbox file not found or failed to read", err, slog.String("file_path", filePath))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
	defer func() {
		_ = file.Close()
	}()

	header, err := headers.ReadHeader(file)
	if err != nil {
		span.Error("failed to read file header", err, slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	newHeader, err := api.reencryptHeader(ctx, header, c4ghPubKey)
	if err != nil {
		span.Error("failed to reencrypt header", err, slog.String("file_id", fileID), slog.String("c4gh_public_key", c4ghPubKey))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", path.Base(filePath)))

	reader := io.MultiReader(bytes.NewReader(newHeader), file)
	_, err = io.Copy(w, reader)
	if err != nil {
		span.Error("error occurred while sending stream", err, slog.String("file_id", fileID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "setAccession")
	defer span.End()

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
			span.Warn("provided fileid could not be parsed as valid uuid", slog.String("file_id", fileID))
			writeJSON(w, http.StatusBadRequest, "invalid fileid")

			return
		}

		fileDetails, err = api.db.GetFileDetails(ctx, fileID, "verified")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				span.Warn("file details not found", slog.String("file_id", fileID))
				writeJSON(w, http.StatusBadRequest, "file details not found")

				return
			}
			span.Error("failed to get file details", err, slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(ctx, fileID)
		if err != nil {
			span.Error("failed to get decrypted checksum", err, slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		accession.AccessionID = accessionID
		accession.User = fileDetails.User
		accession.FilePath = fileDetails.Path
		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	case hasBody:
		if err := json.NewDecoder(r.Body).Decode(&accession); err != nil {
			span.Warn("could not decode request body", slog.Any("error", err))
			writeJSON(w, http.StatusBadRequest, "invalid request body")

			return
		}

		var err error
		fileID, err = api.db.GetFileIDByUserPathAndStatus(ctx, accession.User, accession.FilePath, "verified")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				span.Warn("file not found", slog.String("user", accession.User), slog.String("file_path", accession.FilePath))
				writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

				return
			}
			span.Error("failed to get file id by user, path, and status", err, slog.String("user", accession.User), slog.String("file_path", accession.FilePath))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		fileDecrChecksum, err := api.db.GetDecryptedChecksum(ctx, fileID)
		if err != nil {
			span.Error("failed to get decrypted checksums", err, slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileDecrChecksum}}

	default:
		span.Warn("required parameters not found")
		writeJSON(w, http.StatusBadRequest, "required parameters not found, need either query parameters (fileid, accessionid) or httpbody (filepath & accession_id)")

		return
	}

	accession.Type = "accession"
	marshaledMsg, _ := json.Marshal(&accession)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		span.Error("could not validate accession message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if err := api.mq.Publish(ctx, "accession", broker.Message{Key: fileID, Body: marshaledMsg}); err != nil {
		span.Error("failed to publish accession message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "createDataset")
	defer span.End()

	var dataset dataset
	if err := json.NewDecoder(r.Body).Decode(&dataset); err != nil {
		span.Warn("could not decode request body", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if len(dataset.AccessionIDs) == 0 {
		span.Warn("no accession IDs received")
		writeJSON(w, http.StatusBadRequest, "no accession IDs received")

		return
	}

	// Validate that the files to have overridden download paths are added to the dataset in the same request
	for accessionToSetDownloadPath, downloadPath := range dataset.FileDownloadPaths {
		if downloadPath == "" {
			span.Warn("empty download path for file", slog.String("accession", accessionToSetDownloadPath))
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
			span.Warn("attempted to set file download path for a file not being added to the dataset")
			writeJSON(w, http.StatusBadRequest, "attempted to set file download path for a file not being added to the dataset")

			return
		}
	}

	// Check that the files the accession ids are linked to belong to the user of the dataset
	for _, accessionID := range dataset.AccessionIDs {
		md, err := api.db.GetMappingData(ctx, accessionID)
		if err != nil {
			span.Error("failed to get mapping data", err, slog.String("accession", accessionID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}
		if md == nil {
			span.Warn("rejecting create dataset request including non-existing id", slog.String("accession", accessionID))
			writeJSON(w, http.StatusBadRequest, "request contains non existing file")

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
		span.Error("failed to validate dataset mapping message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	mappingsMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(ctx, "mappings", mappingsMessage); err != nil {
		span.Error("failed to publish mappings message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) releaseDataset(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "releaseDataset")
	defer span.End()

	datasetID := r.PathValue("datasetid")
	ok, err := api.db.CheckIfDatasetExists(ctx, datasetID)
	if err != nil {
		span.Error("failed to check if dataset exists", err, slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
	if !ok {
		span.Warn("dataset not found", slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

		return
	}

	status, err := api.db.GetDatasetStatus(ctx, datasetID)
	if err != nil {
		span.Error("failed to get dataset status", err, slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
	if status != "registered" {
		span.Warn("dataset not registered", slog.String("dataset_id", datasetID), slog.String("status", status))
		writeJSON(w, http.StatusBadRequest, "dataset not ready for release")

		return
	}

	datasetMsg := schema.DatasetRelease{
		Type:      "release",
		DatasetID: datasetID,
	}
	marshaledMsg, _ := json.Marshal(&datasetMsg)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/dataset-release.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		span.Error("could not validate dataset release message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	releaseMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(ctx, "mappings", releaseMessage); err != nil {
		span.Error("failed to publish dataset release message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyFile(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "rotateKeyFile")
	defer span.End()

	fileID := r.PathValue("fileid")

	if _, err := uuid.Parse(fileID); err != nil {
		span.Warn("invalid file ID", slog.Any("error", err), slog.String("file_id", fileID))
		writeJSON(w, http.StatusBadRequest, "invalid file id")

		return
	}

	rotateMsg := schema.KeyRotation{
		Type:   "key_rotation",
		FileID: fileID,
	}

	marshaledMsg, err := json.Marshal(&rotateMsg)
	if err != nil {
		span.Error("could not marshal rotate key message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
		span.Error("could not validate rotate key message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	rotateMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(ctx, "rotatekey", rotateMessage); err != nil {
		span.Error("failed to publish rotate key message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) rotateKeyDataset(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "rotateKeyDataset")
	defer span.End()

	datasetID := r.PathValue("datasetid")
	if datasetID == "" {
		span.Warn("dataset id missing from request")
		writeJSON(w, http.StatusBadRequest, "dataset id missing")

		return
	}

	exists, err := api.db.CheckIfDatasetExists(ctx, datasetID)
	if err != nil {
		span.Error("failed to check if dataset exists", err, slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
	if !exists {
		span.Warn("dataset not found", slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

		return
	}

	files, err := api.db.GetDatasetFileIDs(ctx, datasetID)
	if err != nil {
		span.Error("failed to get dataset files for dataset", err, slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if len(files) == 0 {
		span.Warn("no files found for dataset", slog.String("dataset_id", datasetID))
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
			span.Error("failed to marshal rotate key message", err, slog.String("dataset_id", datasetID), slog.String("file_id", fileID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		if err := schema.ValidateJSON(fmt.Sprintf("%s/rotate-key.json", apiconfig.SchemaPath()), marshaledMsg); err != nil {
			span.Error("could not validate rotate key message", err)
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		rotateKeyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(ctx, "rotatekey", rotateKeyMessage); err != nil {
			span.Error("failed to publish rotate key message", err)
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}
	}

	span.Info("rotation messages sent", slog.Int("nr_files", len(files)), slog.String("dataset_id", datasetID))
	w.WriteHeader(http.StatusOK)
}

func (api *API) listActiveUsers(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "listActiveUsers")
	defer span.End()

	users, err := api.db.ListActiveUsers(ctx)
	if err != nil {
		span.Error("failed to list active users", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string][]string{"users": users})
}

func (api *API) listUserFiles(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "listUserFiles")
	defer span.End()

	username := r.PathValue("username")
	if username == "" {
		writeJSON(w, http.StatusBadRequest, "missing username")

		return
	}

	// parse optional pagination params
	limit, err := parseLimitParam(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "invalid limit")

		return
	}

	cursor := r.URL.Query().Get("cursor")
	pathPrefix := r.URL.Query().Get("path_prefix")
	span.Info("getting files",
		slog.String("username", username),
		slog.String("pathPrefix", pathPrefix),
		slog.Int("limit", limit),
		slog.String("cursor", cursor),
	)
	files, nextCursor, err := api.db.GetUserFiles(ctx, username, pathPrefix, true, limit, cursor)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			writeJSON(w, http.StatusBadRequest, "invalid cursor parameter")

			return
		}
		span.Error("failed to get user files", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "addC4ghHash")
	defer span.End()

	var c4gh schema.C4ghPubKey
	if err := json.NewDecoder(r.Body).Decode(&c4gh); err != nil {
		span.Warn("could not decode request body", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, "invalid request body")

		return
	}

	b64d, err := base64.StdEncoding.DecodeString(c4gh.PubKey)
	if err != nil {
		span.Warn("could not base64 decode public key", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, "invalid public key")

		return
	}

	pubKey, err := keys.ReadPublicKey(bytes.NewReader(b64d))
	if err != nil {
		span.Warn("could not read public key", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, "invalid public key")

		return
	}

	keyHash := hex.EncodeToString(pubKey[:])
	err = api.db.AddKeyHash(ctx, keyHash, c4gh.Description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Warn("key hash already exists", slog.String("key_hash", keyHash))
			writeJSON(w, http.StatusConflict, "key hash already exists")

			return
		}
		span.Error("failed to insert key hash to database", err, slog.String("key_hash", keyHash))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) listC4ghHashes(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "listC4ghHashes")
	defer span.End()

	hashes, err := api.db.ListKeyHashes(ctx)
	if err != nil {
		span.Error("failed to list c4gh key hashes", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "deprecateC4ghHash")
	defer span.End()

	keyHash := r.PathValue("keyhash")
	err := api.db.DeprecateKeyHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.Warn("key hash not found or already deprecated", slog.String("key_hash", keyHash))
			writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		}
		span.Error("failed to deprecate key hash", err, slog.String("key_hash", keyHash))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}
}

func (api *API) listAllDatasets(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "listAllDatasets")
	defer span.End()

	datasets, err := api.db.ListDatasets(ctx)
	if err != nil {
		span.Error("failed to list datasets", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "listUserDatasets")
	defer span.End()

	username := r.PathValue("username")
	datasets, err := api.db.ListUserDatasets(ctx, username)
	if err != nil {
		span.Error("failed to list users datasets", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "listDatasets")
	defer span.End()

	token, err := api.auth.Authenticate(r)
	if err != nil {
		// This is internal error as rbac middleware should have authenticated it already
		span.Error("failed to authenticate user", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	datasets, err := api.db.ListUserDatasets(ctx, token.Subject())
	if err != nil {
		span.Error("could not list users datasets", err, slog.String("user", token.Subject()))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
	ctx, span := observability.StartSpan(r.Context(), "verifyFile")
	defer span.End()

	accessionID := r.PathValue("accession")
	reverificationData, err := api.db.GetReVerificationData(ctx, accessionID)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			span.Error("accession ID not found", err)
			writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		}
		span.Error("could not retrieve reverification data", err)
		writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

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
		span.Error("could not validate verification message", err)
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	verifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
	if err := api.mq.Publish(ctx, "archived", verifyMessage); err != nil {
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (api *API) verifyDataset(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.StartSpan(r.Context(), "verifyDataset")
	defer span.End()

	datasetID := r.PathValue("dataset")
	files, err := api.db.GetDatasetFiles(ctx, datasetID)
	if err != nil {
		span.Error("failed to get dataset files", err, slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

		return
	}

	if len(files) == 0 {
		span.Warn("no files found for dataset", slog.String("dataset_id", datasetID))
		writeJSON(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

		return
	}

	for _, accessionID := range files {
		reverificationData, err := api.db.GetReVerificationData(ctx, accessionID)
		if err != nil {
			span.Error("could not get reverification data from database", err, slog.String("accession_id", accessionID))
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

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
			span.Error("could not validate verification message", err)
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}

		verifyMessage := broker.Message{Key: "", Headers: nil, Body: marshaledMsg}
		if err := api.mq.Publish(ctx, "archived", verifyMessage); err != nil {
			span.Error("failed to publish verification message", err)
			writeJSON(w, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))

			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
