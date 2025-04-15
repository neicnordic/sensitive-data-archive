package s3

import (
	"database/sql"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/internal/database"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type S3TestSuite struct {
	suite.Suite
	Mock sqlmock.Sqlmock
}

func (suite *S3TestSuite) SetupTest() {
	var err error
	var db *sql.DB

	// create mock database
	testConnInfo := "host=localhost port=5432 user=user password=pass dbname=db sslmode=disable"

	db, suite.Mock, err = sqlmock.New()
	if err != nil {
		suite.T().Fatalf("error '%s' when creating mock database connection", err)
	}

	database.DB = &database.SQLdb{DB: db, ConnInfo: testConnInfo}

	// Substitute mock functions
	auth.GetToken = func(_ http.Header) (string, int, error) {
		return "token", 200, nil
	}
	auth.GetVisas = func(_ auth.OIDCDetails, _ string) (*auth.Visas, error) {
		return &auth.Visas{}, nil
	}
	auth.GetPermissions = func(_ auth.Visas) []string {
		return []string{"dataset1", "dataset10", "https://url/dataset"}
	}
	session.NewSessionKey = func() string {
		return "key"
	}
}

func (suite *S3TestSuite) TearDownTest() {
}

func TestS3TestSuite(t *testing.T) {
	suite.Run(t, new(S3TestSuite))
}

func (suite *S3TestSuite) TestGetBucketLocation() {
	// Send a request through the middleware to get datasets

	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	router.GET("/*path", middleware.TokenMiddleware(), Download)
	router.ServeHTTP(w, httptest.NewRequest("GET", "/?location", nil))

	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.Nil(suite.T(), err, "failed to parse body from location response")
	defer response.Body.Close()

	expected := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<LocationConstraint xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">us-west-2</LocationConstraint>"

	assert.Equal(suite.T(), expected, string(body), "Wrong location from S3")
}

func (suite *S3TestSuite) TestListBuckets() {
	// Setup a mock database to handle queries

	query := `SELECT stable_id, created_at FROM sda.datasets WHERE stable_id = \$1`
	suite.Mock.ExpectQuery(query).WithArgs("dataset1").
		WillReturnRows(sqlmock.NewRows([]string{"stable_id", "created_at"}).AddRow("dataset1", "nyss"))
	suite.Mock.ExpectQuery(query).WithArgs("dataset10").
		WillReturnRows(sqlmock.NewRows([]string{"stable_id", "created_at"}).AddRow("dataset1", "nyligen"))
	suite.Mock.ExpectQuery(query).WithArgs("https://url/dataset").
		WillReturnRows(sqlmock.NewRows([]string{"stable_id", "created_at"}).AddRow("dataset1", "snart"))

	// Send a request through the middleware to get datasets
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)

	router.GET("/*path", middleware.TokenMiddleware(), Download)
	router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.Nil(suite.T(), err, "failed to parse body from location response")
	defer response.Body.Close()

	expected := xml.Header +
		"<ListAllMyBucketsResult><Buckets>" +
		"<Bucket><CreationDate>nyss</CreationDate><Name>dataset1</Name></Bucket>" +
		"<Bucket><CreationDate>nyligen</CreationDate><Name>dataset1</Name></Bucket>" +
		"<Bucket><CreationDate>snart</CreationDate><Name>dataset1</Name></Bucket>" +
		"</Buckets><Owner></Owner></ListAllMyBucketsResult>"

	assert.Equal(suite.T(), expected, string(body), "Wrong bucket list from S3")

	err = suite.Mock.ExpectationsWereMet()
	assert.Nilf(suite.T(), err, "there were unfulfilled expectations: %s", err)
}

func (suite *S3TestSuite) TestListByPrefix() {
	// Setup a mock database to handle queries
	fileInfo := &database.FileInfo{
		FileID:                    "file1",
		DatasetID:                 "dataset1",
		DisplayFileName:           "file.txt",
		FilePath:                  "dir/file.txt",
		EncryptedFileSize:         60,
		EncryptedFileChecksum:     "hash",
		EncryptedFileChecksumType: "sha256",
		DecryptedFileSize:         32,
		DecryptedFileChecksum:     "hash",
		DecryptedFileChecksumType: "sha256",
	}

	userID := "user1"

	query := `
		SELECT files.stable_id AS id,
			datasets.stable_id AS dataset_id,
			reverse\(split_part\(reverse\(files.submission_file_path::text\), '/'::text, 1\)\) AS display_file_name,
			files.submission_user AS user_id,
			files.submission_file_path AS file_path,
			files.archive_file_size AS file_size,
			lef.archive_file_checksum AS encrypted_file_checksum,
			lef.archive_file_checksum_type AS encrypted_file_checksum_type,
			files.decrypted_file_size,
			sha.checksum AS decrypted_file_checksum,
			sha.type AS decrypted_file_checksum_type
		FROM sda.files
		JOIN sda.file_dataset ON file_id = files.id
		JOIN sda.datasets ON file_dataset.dataset_id = datasets.id
		LEFT JOIN local_ega.files lef ON files.stable_id = lef.stable_id
		LEFT JOIN \(SELECT file_id, \(ARRAY_AGG\(event ORDER BY started_at DESC\)\)\[1\] AS event FROM sda.file_event_log GROUP BY file_id\) log ON files.id = log.file_id
		LEFT JOIN \(SELECT file_id, checksum, type FROM sda.checksums WHERE source = 'UNENCRYPTED'\) sha ON files.id = sha.file_id
		WHERE datasets.stable_id = \$1;
		`
	suite.Mock.ExpectQuery(query).
		WithArgs("dataset1").
		WillReturnRows(sqlmock.NewRows([]string{"file_id", "dataset_id",
			"display_file_name", "user_id", "file_path", "file_size",
			"decrypted_file_checksum", "decrypted_file_checksum_type",
			"decrypted_file_size", "decrypted_file_checksum",
			"decrypted_file_checksum_type"}).AddRow(fileInfo.FileID, fileInfo.DatasetID,
			fileInfo.DisplayFileName, userID, fileInfo.FilePath,
			fileInfo.EncryptedFileSize, fileInfo.EncryptedFileChecksum, fileInfo.EncryptedFileChecksumType, fileInfo.DecryptedFileSize,
			fileInfo.DecryptedFileChecksum, fileInfo.DecryptedFileChecksumType))

	// Send a request through the middleware to get files for the dataset and
	// prefix

	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)

	router.GET("/*path", middleware.TokenMiddleware(), Download)
	router.ServeHTTP(w, httptest.NewRequest("GET", "/dataset1/?prefix=fi", nil))

	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.Nil(suite.T(), err, "failed to parse body from location response")
	defer response.Body.Close()

	expected := xml.Header +
		"<ListBucketResult><CommonPrefixes></CommonPrefixes><Contents>" +
		"<Key>file.txt</Key>" +
		"<Owner></Owner>" +
		"<Size>32</Size>" +
		"</Contents>" +
		"<Name>dataset1</Name>" +
		"</ListBucketResult>"

	assert.Equal(suite.T(), expected, string(body), "Wrong object list from S3")

	err = suite.Mock.ExpectationsWereMet()
	assert.Nilf(suite.T(), err, "there were unfulfilled expectations: %s", err)
}

func (suite *S3TestSuite) TestListObjects() {
	// Setup a mock database to handlequeries
	fileInfo := &database.FileInfo{
		FileID:                    "file1",
		DatasetID:                 "dataset1",
		DisplayFileName:           "file.txt",
		FilePath:                  "dir/file.txt",
		EncryptedFileSize:         60,
		EncryptedFileChecksum:     "hash",
		EncryptedFileChecksumType: "sha256",
		DecryptedFileSize:         32,
		DecryptedFileChecksum:     "hash",
		DecryptedFileChecksumType: "sha256",
	}

	userID := "user1"

	query := `
		SELECT files.stable_id AS id,
			datasets.stable_id AS dataset_id,
			reverse\(split_part\(reverse\(files.submission_file_path::text\), '/'::text, 1\)\) AS display_file_name,
			files.submission_user AS user_id,
			files.submission_file_path AS file_path,
			files.archive_file_size AS file_size,
			lef.archive_file_checksum AS encrypted_file_checksum,
			lef.archive_file_checksum_type AS encrypted_file_checksum_type,
			files.decrypted_file_size,
			sha.checksum AS decrypted_file_checksum,
			sha.type AS decrypted_file_checksum_type
		FROM sda.files
		JOIN sda.file_dataset ON file_id = files.id
		JOIN sda.datasets ON file_dataset.dataset_id = datasets.id
		LEFT JOIN local_ega.files lef ON files.stable_id = lef.stable_id
		LEFT JOIN \(SELECT file_id, \(ARRAY_AGG\(event ORDER BY started_at DESC\)\)\[1\] AS event FROM sda.file_event_log GROUP BY file_id\) log ON files.id = log.file_id
		LEFT JOIN \(SELECT file_id, checksum, type FROM sda.checksums WHERE source = 'UNENCRYPTED'\) sha ON files.id = sha.file_id
		WHERE datasets.stable_id = \$1;
		`
	suite.Mock.ExpectQuery(query).
		WithArgs("dataset1").
		WillReturnRows(sqlmock.NewRows([]string{"file_id", "dataset_id",
			"display_file_name", "user_id", "file_path", "file_size",
			"encrypted_file_checksum", "encrypted_file_checksum_type",
			"decrypted_file_size", "decrypted_file_checksum",
			"decrypted_file_checksum_type"}).AddRow(fileInfo.FileID, fileInfo.DatasetID,
			fileInfo.DisplayFileName, userID, fileInfo.FilePath,
			fileInfo.EncryptedFileSize, fileInfo.EncryptedFileChecksum, fileInfo.EncryptedFileChecksumType, fileInfo.DecryptedFileSize,
			fileInfo.DecryptedFileChecksum, fileInfo.DecryptedFileChecksumType))

	// Send a request through the middleware to get datasets

	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)

	router.GET("/*path", middleware.TokenMiddleware(), Download)
	router.ServeHTTP(w, httptest.NewRequest("GET", "/dataset1", nil))

	response := w.Result()
	body, err := io.ReadAll(response.Body)
	assert.Nil(suite.T(), err, "failed to parse body from location response")
	defer response.Body.Close()

	expected := xml.Header +
		"<ListBucketResult><CommonPrefixes></CommonPrefixes><Contents>" +
		"<Key>file.txt</Key>" +
		"<Owner></Owner>" +
		"<Size>32</Size>" +
		"</Contents>" +
		"<Name>dataset1</Name>" +
		"</ListBucketResult>"

	assert.Equal(suite.T(), expected, string(body), "Wrong object list from S3")

	err = suite.Mock.ExpectationsWereMet()
	assert.Nilf(suite.T(), err, "there were unfulfilled expectations: %s", err)
}

func (suite *S3TestSuite) TestParseParams() {
	type paramTest struct {
		Path     string
		Dataset  string
		Filename string
	}

	testParams := []paramTest{
		{Path: "/dataset1", Dataset: "dataset1", Filename: ""},
		{Path: "/dataset10", Dataset: "dataset10", Filename: ""},
		{Path: "/dataset1/dir/file.txt", Dataset: "dataset1", Filename: "dir/file.txt"},
		{Path: "/dataset10/file.txt", Dataset: "dataset10", Filename: "file.txt"},
		{Path: "/https://url/dataset/dir/file.txt", Dataset: "https://url/dataset", Filename: "dir/file.txt"},
		{Path: "/https:/url/dataset/dir/file.txt", Dataset: "https://url/dataset", Filename: "dir/file.txt"},
		{Path: "/https%3A%2F%2Furl%2Fdataset/file.txt", Dataset: "https://url/dataset", Filename: "file.txt"},
		{Path: "/https%3A%2Furl%2Fdataset/file.txt", Dataset: "https://url/dataset", Filename: "file.txt"},
	}

	for _, params := range testParams {
		// response function to check parameter parsing
		testParseParams := func(c *gin.Context) {
			parseParams(c)

			assert.Equal(suite.T(), params.Dataset, c.Param("dataset"), "Failed to parse dataset name")
			assert.Equal(suite.T(), params.Filename, c.Param("filename"), "Failed to parse file name")
			c.AbortWithStatus(http.StatusAccepted)
		}

		// Send a request through the middleware to get datasets, then call the
		// test function to test parameter parsing

		w := httptest.NewRecorder()
		_, router := gin.CreateTestContext(w)
		router.GET("/*path", middleware.TokenMiddleware(), testParseParams)
		router.ServeHTTP(w, httptest.NewRequest("GET", params.Path, nil))

		response := w.Result()
		defer response.Body.Close()

		assert.Equal(suite.T(), http.StatusAccepted, response.StatusCode, "Request failed")
	}
}
