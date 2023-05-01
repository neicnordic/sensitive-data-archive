package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NBISweden/S3-Upload-Proxy/helper"
	common "github.com/neicnordic/sda-common/database"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/minio/minio-go/v6/pkg/s3signer"
	log "github.com/sirupsen/logrus"
)

// Proxy represents the toplevel object in this application
type Proxy struct {
	s3        S3Config
	auth      Authenticator
	messenger Messenger
	database  *common.SDAdb
	client    *http.Client
	fileIds   map[string]string
}

// S3RequestType is the type of request that we are currently proxying to the
// backend
type S3RequestType int

// The different types of requests
const (
	MakeBucket S3RequestType = iota
	RemoveBucket
	List
	Put
	Get
	Delete
	AbortMultipart
	Policy
	Other
)

// NewProxy creates a new S3Proxy. This implements the ServerHTTP interface.
func NewProxy(s3conf S3Config, auth Authenticator, messenger Messenger, database *common.SDAdb, tls *tls.Config) *Proxy {
	tr := &http.Transport{TLSClientConfig: tls}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	return &Proxy{s3conf, auth, messenger, database, client, make(map[string]string)}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch t := p.detectRequestType(r); t {
	case MakeBucket, RemoveBucket, Delete, Policy, Get:
		// Not allowed
		log.Debug("not allowed known")
		p.notAllowedResponse(w, r)
	case Put, List, Other, AbortMultipart:
		// Allowed
		p.allowedResponse(w, r)
	default:
		log.Debugf("Unexpected request (%v) not allowed", r)
		p.notAllowedResponse(w, r)
	}
}

func (p *Proxy) internalServerError(w http.ResponseWriter, r *http.Request) {
	log.Debugf("Internal server error for request (%v)", r)
	w.WriteHeader(http.StatusInternalServerError)
}

func (p *Proxy) notAllowedResponse(w http.ResponseWriter, _ *http.Request) {
	log.Debug("not allowed response")
	w.WriteHeader(http.StatusForbidden)
}

func (p *Proxy) notAuthorized(w http.ResponseWriter, _ *http.Request) {
	log.Debug("not authorized")
	w.WriteHeader(http.StatusUnauthorized)
}

func (p *Proxy) allowedResponse(w http.ResponseWriter, r *http.Request) {
	claims, err := p.auth.Authenticate(r)
	if err != nil {
		log.Debugf("Request not authenticated (%v)", err)
		p.notAuthorized(w, r)

		return
	}

	log.Debug("prepend")
	p.prependBucketToHostPath(r)

	username := fmt.Sprintf("%v", claims["sub"])
	rawFilepath := strings.Replace(r.URL.Path, "/"+p.s3.bucket+"/", "", 1)

	filepath, err := helper.FormatUploadFilePath(rawFilepath)
	if err != nil {
		log.Debugf(err.Error())
		w.WriteHeader(http.StatusNotAcceptable)

		return
	}

	// register file in database if it's the start of an upload
	if p.detectRequestType(r) == Put && p.fileIds[r.URL.Path] == "" {
		log.Debugf("registering file %v in the database", r.URL.Path)
		p.fileIds[r.URL.Path], err = p.database.RegisterFile(filepath, username)
		log.Debugf("fileId: %v", p.fileIds[r.URL.Path])
		if err != nil {
			log.Errorf("failed to register file in database: %v", err)

			return
		}
	}

	log.Debug("Forwarding to backend")
	s3response, err := p.forwardToBackend(r)

	if err != nil {
		log.Debugf("forwarding error: %v", err)
		p.internalServerError(w, r)

		return
	}

	// Send message to upstream and set file as uploaded in the database
	if p.uploadFinishedSuccessfully(r, s3response) {
		log.Debug("create message")
		message, _ := p.CreateMessageFromRequest(r, claims)
		jsonMessage, err := json.Marshal(message)
		if err != nil {
			log.Errorf("failed to marshal rabbitmq message to json: %v", err)

			return
		}

		switch p.messenger.IsConnClosed() {
		case true:
			log.Errorln("connection is closed")
			w.WriteHeader(http.StatusServiceUnavailable)

			tlsBroker, _ := TLSConfigBroker(Conf)
			m, err := NewAMQPMessenger(Conf.Broker, tlsBroker)
			if err == nil {
				p.messenger = m
			}

		case false:
			if err = p.messenger.SendMessage(p.fileIds[r.URL.Path], jsonMessage); err != nil {
				log.Debug("error when sending message")
				log.Error(err)
			}

			log.Debugf("marking file %v as 'uploaded' in database", p.fileIds[r.URL.Path])
			err = p.database.MarkFileAsUploaded(p.fileIds[r.URL.Path], username, string(jsonMessage))
			if err != nil {
				log.Error(err)
			}
		}

		delete(p.fileIds, r.URL.Path)
	}

	// Writing non-200 to the response before the headers propagate the error
	// to the s3cmd client.
	// Writing 200 here breaks uploads though, and writing non-200 codes after
	// the headers results in the error message always being
	// "MD5 Sums don't match!".
	if s3response.StatusCode < 200 || s3response.StatusCode > 299 {
		w.WriteHeader(s3response.StatusCode)
	}

	// Redirect answer
	log.Debug("redirect answer")
	for header, values := range s3response.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}
	_, err = io.Copy(w, s3response.Body)
	if err != nil {
		log.Fatalln("redirect error")
	}

	// Read any remaining data in the connection and
	// Close so connection can be reused.
	_, _ = io.ReadAll(s3response.Body)
	_ = s3response.Body.Close()
}

func (p *Proxy) uploadFinishedSuccessfully(req *http.Request, response *http.Response) bool {
	if response.StatusCode != 200 {
		return false
	}

	switch req.Method {
	case http.MethodPut:
		if !strings.Contains(req.URL.String(), "partNumber") {
			return true
		}

		return false
	case http.MethodPost:
		if strings.Contains(req.URL.String(), "uploadId") {
			return true
		}

		return false
	default:

		return false
	}
}

func (p *Proxy) forwardToBackend(r *http.Request) (*http.Response, error) {

	p.resignHeader(r, p.s3.accessKey, p.s3.secretKey, p.s3.url)

	// Redirect request
	nr, err := http.NewRequest(r.Method, p.s3.url+r.URL.String(), r.Body)
	if err != nil {
		log.Debug("error when redirecting the request")
		log.Debug(err)

		return nil, err
	}
	nr.Header = r.Header
	contentLength, _ := strconv.ParseInt(r.Header.Get("content-length"), 10, 64)
	nr.ContentLength = contentLength

	return p.client.Do(nr)
}

// Add bucket to host path
func (p *Proxy) prependBucketToHostPath(r *http.Request) {
	bucket := p.s3.bucket

	// Extract username for request's url path
	str, err := url.ParseRequestURI(r.URL.Path)
	if err != nil || str.Path == "" {
		log.Errorf("failed to get path from query (%v)", r.URL.Path)
	}
	path := strings.Split(str.Path, "/")
	username := path[1]

	log.Debugf("incoming path: %s", r.URL.Path)
	log.Debugf("incoming raw: %s", r.URL.RawQuery)

	// Restructure request to query the users folder instead of the general bucket
	switch r.Method {
	case http.MethodGet:
		if strings.Contains(r.URL.String(), "?delimiter") {
			r.URL.Path = "/" + bucket + "/"
			if strings.Contains(r.URL.RawQuery, "&prefix") {
				params := strings.Split(r.URL.RawQuery, "&prefix=")
				r.URL.RawQuery = params[0] + "&prefix=" + username + "%2F" + params[1]
			} else {
				r.URL.RawQuery = r.URL.RawQuery + "&prefix=" + username + "%2F"
			}
			log.Debug("new Raw Query: ", r.URL.RawQuery)
		} else if strings.Contains(r.URL.String(), "?location") || strings.Contains(r.URL.String(), "&prefix") {
			r.URL.Path = "/" + bucket + "/"
			log.Debug("new Path: ", r.URL.Path)
		}
	case http.MethodPost:
		r.URL.Path = "/" + bucket + r.URL.Path
		log.Debug("new Path: ", r.URL.Path)
	case http.MethodPut:
		r.URL.Path = "/" + bucket + r.URL.Path
		log.Debug("new Path: ", r.URL.Path)
	}
	log.Infof("User: %v, Request type %v, Path: %v", username, r.Method, r.URL.Path)
}

// Function for signing the headers of the s3 requests
// Used for for creating a signature for with the default
// credentials of the s3 service and the user's signature (authentication)
func (p *Proxy) resignHeader(r *http.Request, accessKey string, secretKey string, backendURL string) *http.Request {
	log.Debugf("Generating resigning header for %s", backendURL)
	r.Header.Del("X-Amz-Security-Token")
	r.Header.Del("X-Forwarded-Port")
	r.Header.Del("X-Forwarded-Proto")
	r.Header.Del("X-Forwarded-Host")
	r.Header.Del("X-Forwarded-For")
	r.Header.Del("X-Original-Uri")
	r.Header.Del("X-Real-Ip")
	r.Header.Del("X-Request-Id")
	r.Header.Del("X-Scheme")
	if strings.Contains(backendURL, "//") {
		host := strings.SplitN(backendURL, "//", 2)
		r.Host = host[1]
	}

	return s3signer.SignV4(*r, accessKey, secretKey, "", p.s3.region)
}

// Not necessarily a function on the struct since it does not use any of the
// members.
func (p *Proxy) detectRequestType(r *http.Request) S3RequestType {
	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect Get")

			return Get
		case strings.Contains(r.URL.String(), "?acl"):
			log.Debug("detect Policy")

			return Policy
		default:
			log.Debug("detect List")

			return List
		}
	case http.MethodDelete:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect RemoveBucket")

			return RemoveBucket
		case strings.Contains(r.URL.String(), "uploadId"):
			log.Debug("detect AbortMultipart")

			return AbortMultipart
		default:
			// Do we allow deletion of files?
			log.Debug("detect Delete")

			return Delete
		}
	case http.MethodPut:
		switch {
		case strings.HasSuffix(r.URL.String(), "/"):
			log.Debug("detect MakeBucket")

			return MakeBucket
		case strings.Contains(r.URL.String(), "?policy"):
			log.Debug("detect Policy")

			return Policy
		default:
			// Should decide if we will handle copy here or through authentication
			log.Debug("detect Put")

			return Put
		}
	default:
		log.Debug("detect Other")

		return Other
	}
}

// CreateMessageFromRequest is a function that can take a http request and
// figure out the correct rabbitmq message to send from it.
func (p *Proxy) CreateMessageFromRequest(r *http.Request, claims jwt.MapClaims) (Event, error) {
	event := Event{}
	checksum := Checksum{}
	var err error

	checksum.Value, event.Filesize, err = p.requestInfo(r.URL.Path)
	if err != nil {
		log.Fatalf("could not get checksum information: %s", err)
	}

	// Case for simple upload
	event.Operation = "upload"
	event.Filepath = strings.Replace(r.URL.Path, "/"+p.s3.bucket+"/", "", 1)
	event.Username = fmt.Sprintf("%v", claims["sub"])
	checksum.Type = "sha256"
	event.Checksum = []interface{}{checksum}
	log.Info("user ", event.Username, " with pilot ", claims["pilot"], " uploaded file ", event.Filepath, " with checksum ", checksum.Value, " at ", time.Now())

	return event, nil
}

// RequestInfo is a function that makes a request to the S3 and collects
// the etag and size information for the uploaded document
func (p *Proxy) requestInfo(fullPath string) (string, int64, error) {
	filePath := strings.Replace(fullPath, "/"+p.s3.bucket+"/", "", 1)
	s, err := p.newSession()
	if err != nil {
		return "", 0, err
	}
	svc := s3.New(s)
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(p.s3.bucket),
		MaxKeys: aws.Int64(1),
		Prefix:  aws.String(filePath),
	}

	result, err := svc.ListObjectsV2(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				log.Debug("bucket not found when listing objects")
				log.Debug(s3.ErrCodeNoSuchBucket, aerr.Error())
			default:
				log.Debug("caught error when listing objects")
				log.Debug(aerr.Error())
			}
		} else {
			log.Debug("error when listing objects")
			log.Debug(err)
		}

		return "", 0, err
	}

	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(*result.Contents[0].ETag, "\"", "")))), *result.Contents[0].Size, nil

}

func (p *Proxy) newSession() (*session.Session, error) {
	var mySession *session.Session
	var err error
	if p.s3.cacert != "" {
		cert, _ := os.ReadFile(p.s3.cacert)
		cacert := bytes.NewReader(cert)
		mySession, err = session.NewSessionWithOptions(session.Options{
			CustomCABundle: cacert,
			Config: aws.Config{
				Region:           aws.String(p.s3.region),
				Endpoint:         aws.String(p.s3.url),
				DisableSSL:       aws.Bool(strings.HasPrefix(p.s3.url, "http:")),
				S3ForcePathStyle: aws.Bool(true),
				Credentials:      credentials.NewStaticCredentials(p.s3.accessKey, p.s3.secretKey, ""),
			}})
		if err != nil {
			return nil, err
		}
	} else {
		mySession, err = session.NewSession(&aws.Config{
			Region:           aws.String(p.s3.region),
			Endpoint:         aws.String(p.s3.url),
			DisableSSL:       aws.Bool(strings.HasPrefix(p.s3.url, "http:")),
			S3ForcePathStyle: aws.Bool(true),
			Credentials:      credentials.NewStaticCredentials(p.s3.accessKey, p.s3.secretKey, ""),
		})
		if err != nil {
			return nil, err
		}
	}

	return mySession, nil
}
