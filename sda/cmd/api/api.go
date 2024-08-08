package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
)

var Conf *config.Config
var err error
var auth *userauth.ValidateFromToken

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

func setup(config *config.Config) *http.Server {
	r := gin.Default()
	r.GET("/ready", readinessResponse)
	r.GET("/files", getFiles)
	// admin endpoints below here
	if len(config.API.Admins) > 0 {
		r.POST("/file/ingest", isAdmin(), ingestFile)                  // start ingestion of a file
		r.POST("/file/accession", isAdmin(), setAccession)             // assign accession ID to a file
		r.POST("/dataset/create", isAdmin(), createDataset)            // maps a set of files to a dataset
		r.POST("/dataset/release/*dataset", isAdmin(), releaseDataset) // Releases a dataset to be accessible
		r.GET("/users", isAdmin(), listActiveUsers)                    // Lists all users
		r.GET("/users/:username/files", isAdmin(), listUserFiles)      // Lists all unmapped files for a user
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	srv := &http.Server{
		Addr:              config.API.Host + ":" + fmt.Sprint(config.API.Port),
		Handler:           r,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      20 * time.Second,
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

	if DBRes := checkDB(Conf.API.DB, 5*time.Millisecond); DBRes != nil {
		log.Debugf("DB connection error :%v", DBRes)
		Conf.API.DB.Reconnect()
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, "")
}

func checkDB(database *database.SDAdb, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if database.DB == nil {
		return fmt.Errorf("database is nil")
	}

	return database.DB.PingContext(ctx)
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

	files, err := Conf.API.DB.GetUserFiles(token.Subject())
	if err != nil {
		// something went wrong with querying or parsing rows
		c.JSON(502, err.Error())

		return
	}

	// Return response
	c.JSON(200, files)
}

func isAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := auth.Authenticate(c.Request)
		if err != nil {
			log.Debugln("bad token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})

			return
		}
		if !slices.Contains(Conf.API.Admins, token.Subject()) {
			log.Debugf("%s is not an admin", token.Subject())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})

			return
		}
	}
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

	corrID, err := Conf.API.DB.GetCorrID(ingest.User, ingest.FilePath)
	if err != nil {
		switch {
		case corrID == "":
			c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())
		default:
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

	corrID, err := Conf.API.DB.GetCorrID(accession.User, accession.FilePath)
	if err != nil {
		switch {
		case corrID == "":
			c.AbortWithStatusJSON(http.StatusBadRequest, err.Error())
		default:
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
	var dataset schema.DatasetMapping
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

	dataset.Type = "mapping"
	marshaledMsg, _ := json.Marshal(&dataset)
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
	datasetMsg := schema.DatasetRelease{
		Type:      "release",
		DatasetID: strings.TrimPrefix(c.Param("dataset"), "/"),
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

func listUserFiles(c *gin.Context) {
	username := c.Param("username")
	username = strings.TrimPrefix(username, "/")
	username = strings.TrimSuffix(username, "/files")
	log.Debugln(username)
	files, err := Conf.API.DB.GetUserFiles(strings.ReplaceAll(username, "@", "_"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, err.Error())

		return
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.JSON(200, files)
}
