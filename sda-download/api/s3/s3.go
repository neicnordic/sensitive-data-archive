package s3

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/api/sda"
	"github.com/neicnordic/sda-download/internal/database"
	log "github.com/sirupsen/logrus"
)

type LocationConstraint struct {
	XMLns    string `xml:"xmlns,attr"`
	Location string `xml:",innerxml"`
}

type Bucket struct {
	CreationDate string `xml:"CreationDate,omitempty"`
	Name         string `xml:"Name"`
}

type Owner struct {
	DisplayName string `xml:"DisplayName,omitempty"`
	ID          string `xml:"ID,omitempty"`
}

type ListAllMyBucketsResult struct {
	Buckets []Bucket `xml:"Buckets>Bucket"`
	Owner   Owner    `xml:"Owner"`
}

type Object struct {
	ChecksumAlgorithm []string `xml:"ChecksumAlgorithm,omitempty"`
	ETag              string   `xml:"ETag,omitempty"`
	Key               string   `xml:"Key"`
	Owner             Owner    `xml:"Owner,omitempty"`
	Size              int      `xml:"Size,omitempty"`
	StorageClass      string   `xml:"StorageClass,omitempty"`
}

type ListBucketResult struct {
	CommonPrefixes []string `xml:"CommonPrefixes>CommonPrefix"`
	Contents       []Object `xml:"Contents"`
	Delimiter      string   `xml:"Delimiter,omitempty"`
	EncodingType   string   `xml:"EncodingType,omitempty"`
	IsTruncated    bool     `xml:"IsTruncated,omitempty"`
	Marker         string   `xml:"Marker,omitempty"`
	MaxKeys        int      `xml:"MaxKeys,omitempty"`
	Name           string   `xml:"Name"`
	NextMarker     string   `xml:"NextMarker,omitempty"`
	Prefix         string   `xml:"Prefix,omitempty"`
}

// GetBucketLocation respondes to an S3 GetBucketLocation request. This request
// only contains the AWS region name as XML.
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetBucketLocation.html
func GetBucketLocation(c *gin.Context) {
	log.Debug("S3 GetBucketLocation request")

	// Gin doesn't write the xml header when using c.XML, so we add it manually
	_, err := c.Writer.Write([]byte(xml.Header))
	if err != nil {
		log.Errorf("Failed writing XML header: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	c.XML(http.StatusAccepted, LocationConstraint{
		XMLns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Location: "us-west-2",
	})
}

// ListBuckets respondes to an S3 ListBuckets request. This request returns the
// available S3 buckets. We use this to list accessible datasets.
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListBuckets.html
func ListBuckets(c *gin.Context) {
	log.Debug("S3 ListBuckets request")

	// Gin doesn't write the xml header when using c.XML, so we add it manually
	_, err := c.Writer.Write([]byte(xml.Header))
	if err != nil {
		log.Errorf("Failed writing XML header: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	buckets := []Bucket{}
	cache := middleware.GetCacheFromContext(c)
	for _, dataset := range cache.Datasets {
		datasetInfo, err := database.GetDatasetInfo(dataset)
		if err != nil {
			log.Errorf("Failed to get dataset information: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
		}
		// TODO: Add real creation date
		buckets = append(buckets, Bucket{
			Name:         datasetInfo.DatasetID,
			CreationDate: datasetInfo.CreatedAt,
		})
	}

	c.XML(http.StatusAccepted, ListAllMyBucketsResult{
		Buckets: buckets,
		Owner:   Owner{DisplayName: "", ID: ""},
	})
}

// ListObjects respondes to an S3 ListObjects request. This request
// lists the contents of an S3 bucket. We use this to return dataset content.
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjects.html
func ListObjects(c *gin.Context) {
	log.Debug("S3 ListObjects request")

	dataset := c.Param("dataset")

	allowed := false
	cache := middleware.GetCacheFromContext(c)
	for _, known := range cache.Datasets {
		if dataset == known {
			allowed = true

			break
		}
	}
	if !allowed {
		c.AbortWithStatus(http.StatusNotFound)

		return
	}

	files, err := database.GetFiles(dataset)
	if err != nil {
		log.Errorf("Failed getting dataset files: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	// We return the full upload path, as file key
	objects := []Object{}
	for _, file := range files {
		key := strings.TrimSuffix(file.FilePath, ".c4gh")
		// The first part of the upload path is the user id, which should be
		// removed
		parts := strings.Split(key, "/")
		if len(parts) > 1 {
			parts = parts[1:]
		}
		key = strings.Join(parts, "/")

		if !strings.HasPrefix(key, c.Param("prefix")) {
			continue
		}
		if err != nil {
			log.Errorf("failed to parse last modified time: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)

			return
		}
		objects = append(objects, Object{
			Key:  key,
			Size: int(file.DecryptedFileSize),
		})
	}

	// Gin doesn't write the xml header when using c.XML, so we add it manually
	_, err = c.Writer.Write([]byte(xml.Header))
	if err != nil {
		log.Errorf("Failed writing XML header: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	c.XML(http.StatusAccepted, ListBucketResult{
		Name:     dataset,
		Contents: objects,
	})
}

func getFileInfo(c *gin.Context) (fileInfo *database.FileInfo, err error) {
	// Get file info for the given file path (or abort)
	filename := c.Param("filename")
	if !strings.HasSuffix(c.Param("filename"), ".c4gh") {
		filename = c.Param("filename") + ".c4gh"
	}
	fileInfo, err = database.GetDatasetFileInfo(c.Param("dataset"), filename)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			c.AbortWithStatus(http.StatusNotFound)
		} else {
			c.AbortWithStatus(http.StatusInternalServerError)
		}

		return
	}

	return fileInfo, nil
}

// GetObject respondes to an S3 GetObject request. This request returns S3
// objects. This is done by first fetching any file that matches the dataset +
// filename request from the database and then passing the fileID to the
// SDA Download function.
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObject.html
func GetObject(c *gin.Context) {
	log.Debugf("S3 GetObject request, context: %v", c.Params)

	fileInfo, err := getFileInfo(c)
	if err != nil {
		return
	}

	// Set a param so that Download knows to add S3 headers
	c.Set("S3", true)

	// set the fileID so that download knows what file to download
	c.Params = append(c.Params, gin.Param{Key: "fileid", Value: fileInfo.FileID})

	// Download the file
	sda.Download(c)
}

// GetEncryptedObject respondes to an S3 GetObject request for encrypted files.
// This request returns S3 objects. This is done by first fetching any file that matches the dataset +
// filename request from the database and then passing the fileID to the
// SDA Download function.
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObject.html
func GetEcnryptedObject(c *gin.Context) {
	log.Debugf("S3 GetEncryptedObject request, context: %v", c.Params)

	fileInfo, err := getFileInfo(c)
	if err != nil {
		return
	}

	// Set a param so that Download knows to add S3 headers
	c.Set("S3", true)

	// set the fileID so that download knows what file to download
	c.Params = append(c.Params, gin.Param{Key: "fileid", Value: fileInfo.FileID})

	// set the encrypted parameter so that download gets the encrypted file instead
	c.Params = append(c.Params, gin.Param{Key: "type", Value: "encrypted"})

	// Download the file
	sda.Download(c)
}

// parseParams attempts to split the "path" param from the router into a dataset
// name and file path. This is non-trivial as the dataset name is often a url
// which can include slashes, so finding the separation between filename and
// dataset name is done by comparing to accessible datasets.
func parseParams(c *gin.Context) *gin.Context {

	// When doing list requests from s3cmd, the tool will assume that the first
	// slash delimits the bucket, and the rest is the prefix. We use this to
	// "restore" the path in case the prefix param is set.
	path := c.Param("path") + c.Query("prefix")

	// Trim slashes from the path, as the dataset names don't have slashes
	path = strings.Trim(path, "/")

	path, err := url.QueryUnescape(path)
	if err != nil {
		log.Error("Failed to Unescape path")
	}

	// Some tools automatically reduce double slashes to single slashes, which
	// needs to be restored.
	protocolPattern := regexp.MustCompile(`^(https?:/)([^/])`)
	if protocolPattern.Match([]byte(path)) {
		path = string(protocolPattern.ReplaceAll([]byte(path), []byte("$1/$2")))
	}

	cache := middleware.GetCacheFromContext(c)
	for _, dataset := range cache.Datasets {
		// check that the path starts with the dataset name, but also that the
		// path is only the dataset, or that the following character is a slash.
		// This prevents wrong matches in cases like when one dataset name is a
		// prefix of another one, like "dataset1", and "dataset10".
		if strings.HasPrefix(path, dataset) && (len(path) == len(dataset) || path[len(dataset)] == '/') {
			c.Params = append(c.Params, gin.Param{Key: "dataset", Value: dataset})
			remainder := ""
			if len(path) > len(dataset) {
				remainder = path[len(dataset)+1:]
			}

			key := "filename"
			if c.Query("prefix") != "" {
				key = "prefix"
			}
			c.Params = append(c.Params, gin.Param{Key: key, Value: remainder})

			break
		}
	}

	return c
}

// Download is the main entry function for the S3 functionality. It parses the
// dataset parameter from the request and relays the request to the correct S3
// function.
func Download(c *gin.Context) {
	log.Debugf("S3 request: %v", c.Request)

	// Parses the request path into a dataset and a filename
	c = parseParams(c)

	// Try to figure out what kind of request we're getting.
	// S3 request types are described here:
	// https://docs.aws.amazon.com/AmazonS3/latest/API/Welcome.html
	switch {

	case strings.Contains(c.Request.URL.String(), "?location"):
		GetBucketLocation(c)

	case c.Param("dataset") != "" && c.Param("filename") == "":
		ListObjects(c)

	case c.Param("dataset") == "":
		ListBuckets(c)

	case c.Param("filename") != "":
		if strings.HasPrefix(c.Request.URL.Path, "/s3-encrypted") {
			GetEcnryptedObject(c)
		} else {
			GetObject(c)
		}

	default:
		log.Warningf("Got unknown S3 request: %v", c.Request)
		c.AbortWithStatus(http.StatusBadRequest)
	}
}
