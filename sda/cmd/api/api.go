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
	"strings"
	"syscall"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/jsonadapter"
	"github.com/neicnordic/sensitive-data-archive/internal/reencrypt"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type dataset struct {
	AccessionIDs []string `json:"accession_ids"`
	DatasetID    string   `json:"dataset_id"`
	User         string   `json:"user"`
}

var (
	Conf                      *config.Config
	err                       error
	auth                      *userauth.ValidateFromToken
	reencryptHeaderFunc       = reencryptHeader // for testing purposes
	grpcReencryptHeaderClient = func(c reencrypt.ReencryptClient, ctx context.Context, in *reencrypt.ReencryptRequest, opts ...grpc.CallOption) (*reencrypt.ReencryptResponse, error) {
		return c.ReencryptHeader(ctx, in, opts...) // for testing purposes
	}
)

func main() {
	Conf, err = config.NewConfig("api")
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

	if err := setupJwtAuth(); err != nil {
		log.Fatalf("error when setting up JWT auth, reason %s", err.Error())
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
		log.Infof("Starting web server at https://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServeTLS(Conf.API.ServerCert, Conf.API.ServerKey); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	} else {
		log.Infof("Starting web server at http://%s:%d", Conf.API.Host, Conf.API.Port)
		if err := srv.ListenAndServe(); err != nil {
			shutdown()
			log.Fatalln(err)
		}
	}
}

func setup(conf *config.Config) *http.Server {
	m, _ := model.NewModelFromString(jsonadapter.Model)
	e, err := casbin.NewEnforcer(m, jsonadapter.NewAdapter(&Conf.API.RBACpolicy))
	if err != nil {
		shutdown()
		log.Fatalf("error when setting up RBAC enforcer, reason %s", err.Error()) // nolint # FIXME Fatal should only be called from main
	}

	r := gin.Default()
	r.GET("/ready", readinessResponse)
	r.GET("/files", rbac(e), getFiles)
	r.GET("/datasets", rbac(e), listDatasets)
	// admin endpoints below here
	r.POST("/c4gh-keys/add", rbac(e), addC4ghHash)                      // Adds a key hash to the database
	r.GET("/c4gh-keys/list", rbac(e), listC4ghHashes)                   // Lists key hashes in the database
	r.POST("/c4gh-keys/deprecate/*keyHash", rbac(e), deprecateC4ghHash) // Deprecate a given key hash
	r.DELETE("/file/:username/:fileid", rbac(e), deleteFile)            // Delete a file from inbox
	// submission endpoints below here
	r.POST("/file/ingest", rbac(e), ingestFile)                   // start ingestion of a file
	r.POST("/file/accession", rbac(e), setAccession)              // assign accession ID to a file
	r.PUT("/file/verify/:accession", rbac(e), reVerifyFile)       // trigger reverification of a file
	r.POST("/dataset/create", rbac(e), createDataset)             // maps a set of files to a dataset
	r.POST("/dataset/release/*dataset", rbac(e), releaseDataset)  // Releases a dataset to be accessible
	r.PUT("/dataset/verify/*dataset", rbac(e), reVerifyDataset)   // Re-verify all files in the dataset
	r.GET("/datasets/list", rbac(e), listAllDatasets)             // Lists all datasets with their status
	r.GET("/datasets/list/:username", rbac(e), listUserDatasets)  // Lists datasets with their status for a specific user
	r.GET("/users", rbac(e), listActiveUsers)                     // Lists all users
	r.GET("/users/:username/files", rbac(e), listUserFiles)       // Lists all unmapped files for a user
	r.GET("/users/:username/file/:fileid", rbac(e), downloadFile) // Download a file from a users inbox

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              conf.API.Host + ":" + fmt.Sprint(conf.API.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      2 * time.Minute,
	}

	return srv
}

func setupJwtAuth() error {
	auth = userauth.NewValidateFromToken(jwk.NewSet())
	if Conf.Server.Jwtpubkeyurl != "" {
		if err := auth.FetchJwtPubKeyURL(Conf.Server.Jwtpubkeyurl); err != nil {
			return err
		}
	}
	if Conf.Server.Jwtpubkeypath != "" {
		if err := auth.ReadJwtPubKeyPath(Conf.Server.Jwtpubkeypath); err != nil {
			return err
		}
	}

	return nil
}

func shutdown() {
	defer Conf.API.MQ.Channel.Close()
	defer Conf.API.MQ.Connection.Close()
	defer Conf.API.DB.Close()
}

func readinessResponse(c *gin.Context) {
	statusCode := http.StatusOK

	if Conf.API.MQ.Connection.IsClosed() {
		statusCode = http.StatusServiceUnavailable
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	if Conf.API.MQ.Channel.IsClosed() {
		statusCode = http.StatusServiceUnavailable
		Conf.API.MQ.Connection.Close()
		newConn, err := broker.NewMQ(Conf.Broker)
		if err != nil {
			log.Errorf("failed to reconnect to MQ, reason: %v", err)
		} else {
			Conf.API.MQ = newConn
		}
	}

	if dbRes := checkDB(Conf.API.DB, 5*time.Millisecond); dbRes != nil {
		log.Debugf("DB connection error :%v", dbRes)
		Conf.API.DB.Reconnect()
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, "")
}

func checkDB(db *database.SDAdb, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if db.DB == nil {
		return errors.New("database is nil")
	}

	return db.DB.PingContext(ctx)
}

func rbac(e *casbin.Enforcer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := auth.Authenticate(c.Request)
		if err != nil {
			log.Debugln("bad token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})

			return
		}

		ok, err := e.Enforce(token.Subject(), c.Request.URL.String(), c.Request.Method)
		if err != nil {
			log.Debugf("rbac enforcement failed, reason: %s\n", err.Error())
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})

			return
		}
		log.Debugln("authorized")
	}
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

	files, err := Conf.API.DB.GetUserFiles(token.Subject(), false)
	if err != nil {
		// something went wrong with querying or parsing rows
		c.JSON(502, err.Error())

		return
	}

	// Return response
	c.JSON(200, files)
}

func ingestFile(c *gin.Context) {
	var ingest schema.IngestionTrigger
	if err := c.BindJSON(&ingest); err != nil {
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			gin.H{
				"error":  "json decoding : " + err.Error(),
				"status": http.StatusBadRequest,
			},
		)

		return
	}

	ingest.Type = "ingest"
	marshaledMsg, _ := json.Marshal(&ingest)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-trigger.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}

	corrID, err := Conf.API.DB.GetCorrID(ingest.User, ingest.FilePath, "")
	if err != nil {
		if corrID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())
		}

		return
	}

	err = Conf.API.MQ.SendMessage(corrID, Conf.Broker.Exchange, "ingest", marshaledMsg)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Status(http.StatusOK)
}

// The deleteFile function deletes files from the inbox and marks them as
// discarded in the db. Files are identified by their ids and the user id.
func deleteFile(c *gin.Context) {
	inbox, err := storage.NewBackend(Conf.Inbox)
	if err != nil {
		log.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	submissionUser := c.Param("username")
	log.Debug("submission user:", submissionUser)

	fileID := c.Param("fileid")
	fileID = strings.TrimPrefix(fileID, "/")
	log.Debug("submission file:", fileID)
	if fileID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, "file ID is required")

		return
	}

	// Get the file path from the fileID and submission user
	filePath, err := Conf.API.DB.GetInboxFilePathFromID(submissionUser, fileID)
	if err != nil {
		log.Errorf("getting file from fileID failed, reason: (%v)", err)
		c.AbortWithStatusJSON(http.StatusNotFound, "File could not be found in inbox")

		return
	}

	var retryTimes = 5
	for count := 1; count <= retryTimes; count++ {
		err = inbox.RemoveFile(filePath)
		if err == nil {
			break
		}
		log.Errorf("Remove file from inbox failed, reason: %v", err)
		if count == 5 {
			c.AbortWithStatusJSON(http.StatusInternalServerError, ("remove file from inbox failed"))

			return
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	if err := Conf.API.DB.UpdateFileEventLog(fileID, "disabled", fileID, "api", "{}", "{}"); err != nil {
		log.Errorf("set status deleted failed, reason: (%v)", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Status(http.StatusOK)
}

// reencryptHeader re-encrypts the header of a file using the public key
// provided in the request header and returns the new header. The function uses
// gRPC to communicate with the re-encrypt service and handles TLS configuration
// if needed. The function also handles the case where the CA certificate is
// provided for secure communication.
func reencryptHeader(oldHeader []byte, c4ghPubKey string) ([]byte, error) {
	var opts []grpc.DialOption
	switch {
	case Conf.API.Grpc.ClientCreds != nil:
		opts = append(opts, grpc.WithTransportCredentials(Conf.API.Grpc.ClientCreds))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", Conf.API.Grpc.Host, Conf.API.Grpc.Port), opts...)
	if err != nil {
		log.Errorf("failed to connect to the reencrypt service, reason: %s", err)

		return nil, err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(Conf.API.Grpc.Timeout)*time.Second)
	defer cancel()

	c := reencrypt.NewReencryptClient(conn)
	res, err := c.ReencryptHeader(ctx, &reencrypt.ReencryptRequest{Oldheader: oldHeader, Publickey: c4ghPubKey})
	if err != nil {
		return nil, err
	}

	return res.Header, nil
}

// The downloadFile function download a file re-encrypted with the public key
// provided in the request header from the inbox. It retrieves the file path
// from the database using the file ID and user ID.
func downloadFile(c *gin.Context) {
	// Get the public key from the request header.
	c4ghPubKey := c.GetHeader("C4GH-Public-Key")

	pubKey, err := base64.StdEncoding.DecodeString(c4ghPubKey)
	if err != nil || len(pubKey) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, "bad public key")
		log.Errorf("bad public key, error: %v", err)

		return
	}

	inbox, err := storage.NewBackend(Conf.Inbox)
	if err != nil {
		log.Error(err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	// Retrieve the actual file path for the user's file.
	fileID := strings.TrimPrefix(c.Param("fileid"), "/")
	filePath, err := Conf.API.DB.GetInboxFilePathFromID(
		strings.TrimPrefix(c.Param("username"), "/"),
		fileID,
	)
	if err != nil {
		log.Errorf("getting file path from fileID (%s) failed, reason: (%v)", fileID, err)
		c.AbortWithStatusJSON(http.StatusNotFound, "failed to retrieve inbox file path")

		return
	}

	// Get inbox file handle
	file, err := inbox.NewFileReader(filePath)
	if err != nil {
		log.Errorf("could not find inbox file %s, %s", filePath, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, "storage backend error")

		return
	}

	// get the header of the crypt4gh file
	header, err := headers.ReadHeader(file)
	if err != nil {
		log.Debugf("failed to read header for fileID %s, reason: %v", fileID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, "failed to read the start of the file")

		return
	}

	newHeader, err := reencryptHeaderFunc(header, c4ghPubKey)
	if err != nil {
		log.Errorf("failed to reencrypt header, reason: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, "failed to reencrypt header")

		return
	}

	// Set the headers for the response.
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("filename: %v", path.Base(filePath)))

	reader := io.MultiReader(bytes.NewReader(newHeader), file)
	n, err := io.Copy(c.Writer, reader)
	if err != nil {
		log.Errorf("error occurred while sending stream: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to stream data to client",
			"details": err.Error(),
		})

		return
	}
	log.Debugf("Sent %d bytes", n)

	c.Status(http.StatusOK)
}

func setAccession(c *gin.Context) {
	var accession schema.IngestionAccession
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

	corrID, err := Conf.API.DB.GetCorrID(accession.User, accession.FilePath, "")
	if err != nil {
		if corrID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())
		} else {
			c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())
		}

		return
	}

	fileInfo, err := Conf.API.DB.GetFileInfo(corrID)
	if err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	accession.DecryptedChecksums = []schema.Checksums{{Type: "sha256", Value: fileInfo.DecryptedChecksum}}
	accession.Type = "accession"
	marshaledMsg, _ := json.Marshal(&accession)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-accession.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Debugln(err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}

	err = Conf.API.MQ.SendMessage(corrID, Conf.Broker.Exchange, "accession", marshaledMsg)
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

	for _, stableID := range dataset.AccessionIDs {
		inboxPath, err := Conf.API.DB.GetInboxPath(stableID)
		if err != nil {
			switch {
			case err.Error() == "sql: no rows in result set":
				log.Errorln(err.Error())
				c.AbortWithStatusJSON(http.StatusBadRequest, fmt.Sprintf("accession ID not found: %s", stableID))

				return
			default:
				log.Errorln(err.Error())
				c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

				return
			}
		}
		_, err = Conf.API.DB.GetCorrID(dataset.User, inboxPath, stableID)
		if err != nil {
			switch {
			case err.Error() == "sql: no rows in result set":
				log.Errorln(err.Error())
				c.AbortWithStatusJSON(http.StatusBadRequest, "accession ID owned by other user")

				return
			default:
				log.Errorln(err.Error())
				c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

				return
			}
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
	ok, err := Conf.API.DB.CheckIfDatasetExists(datasetID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
	if !ok {
		c.AbortWithStatusJSON(http.StatusNotFound, "dataset not found")

		return
	}

	status, err := Conf.API.DB.GetDatasetStatus(datasetID)
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

func listActiveUsers(c *gin.Context) {
	users, err := Conf.API.DB.ListActiveUsers()
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
	files, err := Conf.API.DB.GetUserFiles(username, true)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.JSON(200, files)
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

	err = Conf.API.DB.AddKeyHash(hex.EncodeToString(pubKey[:]), c4gh.Description)
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
	hashes, err := Conf.API.DB.ListKeyHashes()
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
	c.JSON(200, hashes)
}

func deprecateC4ghHash(c *gin.Context) {
	keyHash := strings.TrimPrefix(c.Param("keyHash"), "/")
	err = Conf.API.DB.DeprecateKeyHash(keyHash)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())

		return
	}
}

func listAllDatasets(c *gin.Context) {
	datasets, err := Conf.API.DB.ListDatasets()
	if err != nil {
		log.Errorf("ListAllDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	c.JSON(http.StatusOK, datasets)
}

func listUserDatasets(c *gin.Context) {
	username := strings.TrimPrefix(c.Param("username"), "/")
	datasets, err := Conf.API.DB.ListUserDatasets(username)
	if err != nil {
		log.Errorf("ListUserDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	c.JSON(http.StatusOK, datasets)
}

func listDatasets(c *gin.Context) {
	token, err := auth.Authenticate(c.Request)
	if err != nil {
		c.JSON(401, err.Error())

		return
	}
	datasets, err := Conf.API.DB.ListUserDatasets(token.Subject())
	if err != nil {
		log.Errorf("ListDatasets failed, reason: %s", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	c.JSON(http.StatusOK, datasets)
}

func reVerify(c *gin.Context, accessionID string) (*gin.Context, error) {
	reVerify, err := Conf.API.DB.GetReVerificationData(accessionID)
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			c.AbortWithStatusJSON(http.StatusNotFound, "accession ID not found")
		} else {
			log.Errorln("failed to get file data")
			c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())
		}

		return c, err
	}
	corrID, err := Conf.API.DB.GetCorrID(reVerify.User, reVerify.FilePath, accessionID)
	if err != nil {
		log.Errorf("failed to get CorrID for %s, %s", reVerify.User, reVerify.FilePath)
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return c, err
	}

	marshaledMsg, _ := json.Marshal(&reVerify)
	if err := schema.ValidateJSON(fmt.Sprintf("%s/ingestion-verification.json", Conf.Broker.SchemasPath), marshaledMsg); err != nil {
		log.Errorln(err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return c, err
	}

	err = Conf.API.MQ.SendMessage(corrID, Conf.Broker.Exchange, "archived", marshaledMsg)
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
	accessions, err := Conf.API.DB.GetDatasetFiles(dataset)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}
	if accessions == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, "dataset not found")

		return
	}

	for _, accession := range accessions {
		c, err = reVerify(c, accession)
		if err != nil {
			return
		}
	}

	c.Status(http.StatusOK)
}
