package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/minio/minio-go/v6/pkg/signer"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
)

// Proxy represents the toplevel object in this application
type Proxy struct {
	s3Conf    config.S3InboxConf
	s3Client  *s3.Client
	auth      userauth.Authenticator
	messenger *broker.AMQPBroker
	database  *database.SDAdb
	client    *http.Client
}

// The Event struct
type Event struct {
	Operation string `json:"operation"`
	Username  string `json:"user"`
	Filepath  string `json:"filepath"`
	Filesize  int64  `json:"filesize"`
	Checksum  []any  `json:"encrypted_checksums"`
}

// Checksum used in the message
type Checksum struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// S3RequestType is the type of request that we are currently proxying to the
// backend
type S3RequestType int

type ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// The different types of requests
const (
	Unsupported S3RequestType = iota
	ListObjectsV2
	ListObjects
	PutObject
	UploadPart
	CreateMultiPartUpload
	CompleteMultiPartUpload
	ListMultiPartUploads
	ListParts
	AbortMultiPartUpload
	GetBucketLocation
)

// NewProxy creates a new S3Proxy. This implements the ServerHTTP interface.
func NewProxy(s3conf config.S3InboxConf, s3Client *s3.Client, auth userauth.Authenticator, messenger *broker.AMQPBroker, db *database.SDAdb, tlsConf *tls.Config) *Proxy {
	tr := &http.Transport{TLSClientConfig: tlsConf}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	return &Proxy{
		s3Conf:    s3conf,
		s3Client:  s3Client,
		auth:      auth,
		messenger: messenger,
		database:  db,
		client:    client,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, err := p.auth.Authenticate(r)
	if err != nil {
		log.Warnf("unauthorized user attempted: method: %s, path: %s, query: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		reportErrorToClient(http.StatusUnauthorized, "Unauthorized", w)

		return
	}

	s3RequestType := detectS3RequestType(r)
	switch s3RequestType {
	// These actions we just forward to the s3 backend after ensuring that requests have been made user specific by
	// prepareForwardPathAndQuery
	case ListObjects, ListObjectsV2, GetBucketLocation, UploadPart, ListMultiPartUploads, AbortMultiPartUpload, ListParts:
		p.forwardRequest(s3RequestType, w, r, token)
	case PutObject, CreateMultiPartUpload, CompleteMultiPartUpload:
		p.handleUpload(s3RequestType, w, r, token)
	default:
		log.Warnf("user: %s, attempted to do not allowed request: method: %s, path: %s, query: %s", token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery)
		reportErrorToClient(http.StatusForbidden, "Forbidden", w)
	}
}

// Report 500 to the user, log the original error
func (p *Proxy) internalServerError(w http.ResponseWriter, tokenSubject, httpMethod, path, query, err string) {
	log.Errorf("user: %s, method: %s, path: %s, query: %s, encountered internal error: %s", tokenSubject, httpMethod, path, query, err)
	reportErrorToClient(http.StatusInternalServerError, "Internal Error", w)
}

// prepareForwardPathAndQuery prepares the new path and query to be used for the s3 request to be user specific when
// reaching the s3 backend
func (p *Proxy) prepareForwardPathAndQuery(s3RequestType S3RequestType, originPath, originQuery, tokenSubject string) (string, string, error) {
	str, err := url.ParseRequestURI(originPath)
	if err != nil {
		return "", "", err
	}

	if str.Path == "" {
		return "", "", fmt.Errorf("invalid path: %s", originPath)
	}

	if strings.Contains(tokenSubject, "@") {
		tokenSubject = strings.ReplaceAll(tokenSubject, "@", "_")
	}

	path := strings.Split(str.Path, "/")
	if len(path) < 2 || path[1] == "" {
		return "", "", fmt.Errorf("invalid path: %s", originPath)
	}
	userNameInPath := path[1]
	if tokenSubject != userNameInPath {
		return "", "", fmt.Errorf("token supplied username: %s, but URL had: %s", tokenSubject, path[1])
	}

	var newPath, newQuery string
	switch s3RequestType {
	case GetBucketLocation:
		newPath = "/" + p.s3Conf.Bucket
		newQuery = originQuery
	case ListObjects, ListObjectsV2, ListMultiPartUploads:
		newPath = "/" + p.s3Conf.Bucket

		queryValues, err := url.ParseQuery(originQuery)
		if err != nil {
			return "", "", err
		}
		requiredPrefix := tokenSubject + "/"
		existingPrefix := queryValues.Get("prefix")
		switch {
		case existingPrefix == "":
			queryValues.Set("prefix", requiredPrefix)
		case existingPrefix == tokenSubject:
			queryValues.Set("prefix", requiredPrefix)
		case strings.HasPrefix(existingPrefix, requiredPrefix):
			queryValues.Set("prefix", existingPrefix)
		default:
			queryValues.Set("prefix", requiredPrefix+existingPrefix)
		}
		newQuery = queryValues.Encode()
	default:
		newPath = "/" + p.s3Conf.Bucket + originPath
		newQuery = originQuery
	}

	return newPath, newQuery, nil
}

// forwardRequest forwards the request to the s3 backend after making request user specific, then forwards response to client
func (p *Proxy) forwardRequest(s3RequestType S3RequestType, w http.ResponseWriter, r *http.Request, token jwt.Token) {
	var err error
	r.URL.Path, r.URL.RawQuery, err = p.prepareForwardPathAndQuery(s3RequestType, r.URL.Path, r.URL.RawQuery, token.Subject())
	if err != nil {
		log.Warnf("bad request from user %s: %v", token.Subject(), err)
		reportErrorToClient(http.StatusBadRequest, "Bad Request", w)

		return
	}

	s3Response, err := p.forwardRequestToBackend(r)
	if err != nil {
		p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("forwarding error: %v", err))

		return
	}

	if err := p.forwardResponseToClient(s3Response, w); err != nil {
		p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("failed to forward response to client: %v", err))
	}

	_ = s3Response.Body.Close()
}
func (p *Proxy) handleUpload(s3RequestType S3RequestType, w http.ResponseWriter, r *http.Request, token jwt.Token) {
	username := token.Subject()

	var err error
	r.URL.Path, r.URL.RawQuery, err = p.prepareForwardPathAndQuery(s3RequestType, r.URL.Path, r.URL.RawQuery, username)
	if err != nil {
		log.Warnf("bad request from user %s: %v", token.Subject(), err)
		reportErrorToClient(http.StatusBadRequest, "Bad Request", w)

		return
	}

	s3FilePath := strings.Replace(r.URL.Path, "/"+p.s3Conf.Bucket+"/", "", 1)
	filePath, err := formatUploadFilePath(helper.AnonymizeFilepath(s3FilePath, username))
	if err != nil {
		log.Warnf("bad request from user %s: %v", token.Subject(), err)
		reportErrorToClient(http.StatusBadRequest, "Bad Request", w)

		return
	}

	fileID, err := p.database.GetFileIDInInbox(r.Context(), username, filePath)
	if err != nil {
		p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("failed to check/get existing file id from database: %v", err))

		return
	}

	// if this is an upload request
	if fileID == "" {
		fileID, err = p.database.RegisterFile(nil, p.s3Conf.Endpoint+"/"+p.s3Conf.Bucket, filePath, username)
		if err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("failed to register file in database: %v", err))

			return
		}
	}

	isReupload := false
	// check if the file already exists when an upload completes, in that case send an overwrite message when the s3 has responded with 200,
	// so that the FEGA portal is informed that a new version
	if s3RequestType == PutObject || s3RequestType == CompleteMultiPartUpload {
		isReupload, err = p.checkFileExists(r.Context(), s3FilePath)
		if err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, err.Error())

			return
		}
	}

	s3Response, err := p.forwardRequestToBackend(r)
	if err != nil {
		p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("forwarding error: %v", err))

		return
	}
	defer func() {
		_ = s3Response.Body.Close()
	}()

	// Send message to upstream and set file as uploaded in the database when upload is complete(PutObject / CompleteMultipartUpload)
	// nolint: nestif
	if s3Response.StatusCode == 200 && (s3RequestType == PutObject || s3RequestType == CompleteMultiPartUpload) {
		message, checksum, err := p.CreateMessageFromRequest(r.Context(), token.Subject(), s3FilePath)
		if err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, err.Error())

			return
		}
		jsonMessage, err := json.Marshal(message)
		if err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("failed to marshal rabbitmq message to json: %v", err))

			return
		}

		if err = p.checkAndSendMessage(fileID, jsonMessage); err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("broker error: %v", err))

			return
		}

		if err := p.storeObjectSizeInDB(r.Context(), s3FilePath, fileID); err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("storeObjectSizeInDB failed because: %v", err))

			return
		}

		if err := p.database.UpdateFileEventLog(fileID, "uploaded", "inbox", "{}", string(jsonMessage)); err != nil {
			p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("could not connect to db: %v", err))

			return
		}

		if isReupload {
			log.Infof("user: %s, reuploaded file: %s, with id: %s, checksum: %s", username, filePath, fileID, checksum)
			if err := p.sendMessageOnOverwrite(username, fileID, s3FilePath); err != nil {
				p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, err.Error())

				return
			}
		} else {
			log.Infof("user: %s, uploaded file: %s, with id: %s, checksum: %s", username, filePath, fileID, checksum)
		}
	}

	if err := p.forwardResponseToClient(s3Response, w); err != nil {
		p.internalServerError(w, token.Subject(), r.Method, r.URL.Path, r.URL.RawQuery, fmt.Sprintf("failed to forward response to client: %v", err))
	}
}

// Renew the connection to MQ if necessary, then send message
func (p *Proxy) checkAndSendMessage(fileID string, jsonMessage []byte) error {
	var err error
	if p.messenger == nil {
		return errors.New("messenger is down")
	}
	if p.messenger.IsConnClosed() {
		log.Warning("connection is closed, reconnecting...")
		p.messenger, err = broker.NewMQ(p.messenger.Conf)
		if err != nil {
			return err
		}
	}

	if p.messenger.Channel.IsClosed() {
		log.Warning("channel is closed, recreating...")
		err := p.messenger.CreateNewChannel()
		if err != nil {
			return err
		}
	}

	if err := p.messenger.SendMessage(fileID, p.messenger.Conf.Exchange, p.messenger.Conf.RoutingKey, jsonMessage); err != nil {
		return fmt.Errorf("error when sending message to broker: %v", err)
	}

	return nil
}

func (p *Proxy) forwardRequestToBackend(r *http.Request) (*http.Response, error) {
	p.resignHeader(r)
	// Redirect request
	nr, err := http.NewRequest(r.Method, p.s3Conf.Endpoint+r.URL.String(), r.Body) // #nosec G704 -- endpoint and port controlled by configuration
	if err != nil {
		return nil, err
	}
	nr.Header = r.Header
	contentLength, _ := strconv.ParseInt(r.Header.Get("content-length"), 10, 64)
	nr.ContentLength = contentLength

	return p.client.Do(nr) // #nosec G704 -- endpoint and port controlled by configuration
}
func (p *Proxy) forwardResponseToClient(s3Response *http.Response, w http.ResponseWriter) error {
	for header, values := range s3Response.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	// Writing non-200 to the response before the headers propagate the error
	// to the s3cmd client.
	// Writing 200 here breaks uploads though, and writing non-200 codes after
	// the headers results in the error message always being
	// "MD5 Sums don't match!".
	if s3Response.StatusCode < 200 || s3Response.StatusCode > 299 {
		w.WriteHeader(s3Response.StatusCode)
	}

	if _, err := io.Copy(w, s3Response.Body); err != nil {
		return err
	}

	// Read any remaining data in the connection
	_, _ = io.ReadAll(s3Response.Body)

	return nil
}

// Function for signing the headers of the s3 requests
// Used for creating a signature for with the default
// credentials of the s3 service and the user's signature (authentication)
func (p *Proxy) resignHeader(r *http.Request) *http.Request {
	r.Header.Del("X-Amz-Security-Token")
	r.Header.Del("X-Forwarded-Port")
	r.Header.Del("X-Forwarded-Proto")
	r.Header.Del("X-Forwarded-Host")
	r.Header.Del("X-Forwarded-For")
	r.Header.Del("X-Original-Uri")
	r.Header.Del("X-Real-Ip")
	r.Header.Del("X-Request-Id")
	r.Header.Del("X-Scheme")
	if strings.Contains(p.s3Conf.Endpoint, "//") {
		host := strings.SplitN(p.s3Conf.Endpoint, "//", 2)
		r.Host = host[1]
	}

	return signer.SignV4(*r, p.s3Conf.AccessKey, p.s3Conf.SecretKey, "", p.s3Conf.Region)
}

// detectS3RequestType detects which s3 actions is being taken based upon the http method, path and query
// Allowed actions:
//
// * GetBucketLocation == GET /${bucket}?location
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetBucketLocation.html
//
// * ListObjectsV2 == GET /${bucket}?list-type=2
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html
// For ListObjectsV2 we enforce that the prefix query argument starts with the token.Subject() such that a user can
// only see files within a directory named by the token subject
//
// * ListObjects == GET /${bucket}
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjects.html
// Checked by making sure there are no query params that indicate other actions
// Checks that any of the following are not present in the query:
// "acl", "policy", "cors", "lifecycle", "versioning", "logging", "tagging", "encryption", "website", "notification",
//
//	"replication", "analytics", "metrics", "inventory", "ownershipControls", "publicAccessBlock", "object-lock"
//
// * PutObject == PUT /${bucket}/${object}
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html
// partNumber and uploadId query arguments not present
// We ensure x-amz-copy-source is not present to not allow CopyObject
//
// * UploadPart == PUT /${bucket}/${object}
// For aws docs see:  https://docs.aws.amazon.com/AmazonS3/latest/API/API_UploadPart.html
// partNumber and uploadId query arguments present
//
// * CreateMultiPartUpload == POST /${bucket}/${object}?uploads
// For aws docs see:  https://docs.aws.amazon.com/AmazonS3/latest/API/API_CreateMultipartUpload.html
//
// * CompleteMultiPartUpload == POST /${bucket}/${object}?uploadId
// For aws docs see:  https://docs.aws.amazon.com/AmazonS3/latest/API/API_CompleteMultipartUpload.html
//
// * AbortMultiPartUpload == DELETE /${bucket}/${object}?uploadId
// For aws docs see:  https://docs.aws.amazon.com/AmazonS3/latest/API/API_AbortMultipartUpload.html
//
// * ListParts == Get /${bucket}/${object}?uploadId
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListParts.html
//
// * ListMultiPartUploads == Get /${bucket}?uploads
// For aws docs see: https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListMultipartUploads.html
func detectS3RequestType(r *http.Request) S3RequestType {
	query := r.URL.Query()

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	isBucketPath := len(pathParts) == 1 && pathParts[0] != ""
	isObjectPath := len(pathParts) > 1 && pathParts[0] != "" && pathParts[1] != ""

	switch {
	// Unsupported actions
	case query.Has("acl") || query.Has("policy") ||
		query.Has("cors") || query.Has("lifecycle") || query.Has("versioning") ||
		query.Has("logging") || query.Has("tagging") || query.Has("encryption") ||
		query.Has("website") || query.Has("notification") || query.Has("replication") ||
		query.Has("analytics") || query.Has("metrics") || query.Has("inventory") ||
		query.Has("ownershipControls") || query.Has("publicAccessBlock") || query.Has("object-lock"):
		return Unsupported
	// ListObjectsV2
	case r.Method == http.MethodGet && isBucketPath && query.Get("list-type") == "2":
		return ListObjectsV2
	case r.Method == http.MethodGet && isBucketPath && query.Has("uploads"):
		return ListMultiPartUploads
	case r.Method == http.MethodGet && isObjectPath && query.Has("uploadId"):
		return ListParts
	case r.Method == http.MethodGet && isBucketPath && query.Has("location"):
		return GetBucketLocation
	case r.Method == http.MethodGet && isBucketPath:
		return ListObjects
	case r.Method == http.MethodPut && isObjectPath && !query.Has("partNumber") && !query.Has("uploadId") && r.Header.Get("x-amz-copy-source") == "":
		return PutObject
	case r.Method == http.MethodPut && isObjectPath && query.Has("partNumber") && query.Has("uploadId") && r.Header.Get("x-amz-copy-source") == "":
		return UploadPart
	case r.Method == http.MethodPost && isObjectPath && query.Has("uploads"):
		return CreateMultiPartUpload
	case r.Method == http.MethodPost && isObjectPath && query.Has("uploadId"):
		return CompleteMultiPartUpload
	case r.Method == http.MethodDelete && isObjectPath && query.Has("uploadId"):
		return AbortMultiPartUpload
	default:
		return Unsupported
	}
}

// CreateMessageFromRequest is a function that can take a http request and
// figure out the correct rabbitmq message to send from it.
func (p *Proxy) CreateMessageFromRequest(ctx context.Context, username, s3FilePath string) (Event, string, error) {
	event := Event{}
	checksum := Checksum{}
	var err error

	checksum.Value, event.Filesize, err = p.requestInfo(ctx, s3FilePath)
	if err != nil {
		return event, "", fmt.Errorf("could not get checksum information: %s", err)
	}

	// Case for simple upload
	event.Operation = "upload"
	event.Filepath = s3FilePath

	event.Username = username
	checksum.Type = "md5"
	event.Checksum = []any{checksum}

	return event, checksum.Value, nil
}

// RequestInfo is a function that makes a request to the S3 and collects
// the etag and size information for the uploaded document
func (p *Proxy) requestInfo(ctx context.Context, filePath string) (string, int64, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(p.s3Conf.Bucket),
		Key:    aws.String(filePath),
	}

	result, err := p.s3Client.HeadObject(ctx, input)
	if err != nil {
		return "", 0, err
	}

	if result == nil || result.ETag == nil || result.ContentLength == nil {
		return "", 0, errors.New("unexpected response from s3, HeadObject response contains nil information")
	}

	return strings.Trim(*result.ETag, "\""), *result.ContentLength, nil
}

// checkFileExists makes a request to the S3 to check whether the file already
// is uploaded. Returns a bool indicating whether the file was found.
func (p *Proxy) checkFileExists(ctx context.Context, s3FilePath string) (bool, error) {
	result, err := p.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.s3Conf.Bucket),
		Key:    aws.String(s3FilePath),
	})

	if err != nil && strings.Contains(err.Error(), "StatusCode: 404") {
		return false, nil
	}

	return result != nil, err
}

func (p *Proxy) sendMessageOnOverwrite(username, fileID, s3FilePath string) error {
	msg := schema.InboxRemove{
		User:      username,
		FilePath:  s3FilePath,
		Operation: "remove",
	}

	jsonMessage, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	err = p.checkAndSendMessage(fileID, jsonMessage)
	if err != nil {
		return err
	}

	return nil
}

// FormatUploadFilePath ensures that path separators are "/", and returns error if the
// filepath contains a disallowed character matched with regex
func formatUploadFilePath(filePath string) (string, error) {
	// Check for mixed "\" and "/" in filepath. Stop and throw an error if true so that
	// we do not end up with unintended folder structure when applying ReplaceAll below
	if strings.Contains(filePath, "\\") && strings.Contains(filePath, "/") {
		return filePath, errors.New("filepath contains mixed '\\' and '/' characters")
	}

	// make any windows path separators linux compatible
	outPath := strings.ReplaceAll(filePath, "\\", "/")

	// [\x00-\x1F\x7F] is the control character set
	re := regexp.MustCompile(`[\\<>"\|\x00-\x1F\x7F\!\*\'\(\)\;\:\@\&\=\+\$\,\?\%\#\[\]]`)

	disallowedChars := re.FindAllString(outPath, -1)
	if disallowedChars != nil {
		return outPath, fmt.Errorf("filepath contains disallowed characters: %+v", strings.Join(disallowedChars, ", "))
	}

	return outPath, nil
}

// Write the error and its status code to the response
func reportErrorToClient(errorCode int, message string, w http.ResponseWriter) {
	errorResponse := ErrorResponse{
		Code:    http.StatusText(errorCode),
		Message: message,
	}
	w.WriteHeader(errorCode)
	xmlData, err := xml.Marshal(errorResponse)
	if err != nil {
		// errors are logged but otherwised ignored
		log.Error(err)

		return
	}
	// write the error message to the response
	_, err = io.Writer.Write(w, xmlData)
	if err != nil {
		// errors are logged but otherwised ignored
		log.Error(err)
	}
}

func (p *Proxy) storeObjectSizeInDB(ctx context.Context, s3FilePath, fileID string) error {
	o, err := p.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.s3Conf.Bucket),
		Key:    aws.String(s3FilePath),
	})
	if err != nil {
		return err
	}

	if o.ContentLength == nil {
		return errors.New("s3 content length not available")
	}

	return p.database.SetSubmissionFileSize(fileID, *o.ContentLength)
}
