package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ProxyTests struct {
	suite.Suite
	S3Fakeconf storage.S3Conf // fakeserver
	S3conf     storage.S3Conf // actual s3 container
	DBConf     database.DBConf
	fakeServer *FakeServer
	MQConf     broker.MQConf
	messenger  *broker.AMQPBroker
	database   *database.SDAdb
	auth       *userauth.ValidateFromToken
	token      jwt.Token
}

func TestProxyTestSuite(t *testing.T) {
	suite.Run(t, new(ProxyTests))
}

func (suite *ProxyTests) SetupTest() {

	// Create fake server
	suite.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	suite.S3Fakeconf = storage.S3Conf{
		URL:       "http://127.0.0.1",
		Port:      9024,
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}

	suite.S3conf = storage.S3Conf{
		URL:       "http://127.0.0.1",
		Port:      s3Port,
		AccessKey: "access",
		SecretKey: "secretKey",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}

	// Create a configuration for the fake MQ
	suite.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     MQport,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	suite.messenger = &broker.AMQPBroker{}

	// Create a database configuration for the fake database
	suite.DBConf = database.DBConf{
		Host:       "127.0.0.1",
		Port:       DBport,
		User:       "postgres",
		Password:   "rootpasswd",
		Database:   "sda",
		CACert:     "",
		SslMode:    "disable",
		ClientCert: "",
		ClientKey:  "",
	}

	suite.database = &database.SDAdb{}

	// Create temp demo rsa key pair
	demoKeysPath := "demo-rsa-keys"
	defer os.RemoveAll(demoKeysPath)
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(suite.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(suite.T(), err)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateRSAKey(prKeyPath, "/rsa")
	assert.NoError(suite.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"
	suite.auth = userauth.NewValidateFromToken(jwk.NewSet())
	_ = suite.auth.ReadJwtPubKeyPath(jwtpubkeypath)

	suite.token, err = jwt.Parse([]byte(defaultToken), jwt.WithKeySet(suite.auth.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	if err != nil {
		suite.T().FailNow()
	}

	s3cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(suite.S3conf.AccessKey, suite.S3conf.SecretKey, "")))
	if err != nil {
		suite.FailNow("bad")
	}
	s3Client := s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(fmt.Sprintf("%s:%d", suite.S3conf.URL, suite.S3conf.Port))
			o.EndpointOptions.DisableHTTPS = strings.HasPrefix(suite.S3conf.URL, "http:")
			o.Region = suite.S3conf.Region
			o.UsePathStyle = true
		},
	)
	_, _ = s3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{Bucket: aws.String(suite.S3conf.Bucket)})
	if err != nil {
		fmt.Println(err.Error())
	}

	output, err := s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Body:            strings.NewReader("This is a test"),
		Bucket:          aws.String(suite.S3conf.Bucket),
		Key:             aws.String("/dummy/file"),
		ContentEncoding: aws.String("application/octet-stream"),
	})
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), output, output)
}

func (suite *ProxyTests) TearDownTest() {
	suite.fakeServer.Close()
	suite.database.Close()
}

type FakeServer struct {
	ts     *httptest.Server
	resp   string
	pinged bool
}

func startFakeServer(port string) *FakeServer {
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		panic(fmt.Errorf("can't create mock server for testing: %s", err))
	}
	log.Warnf("fake server running")
	f := FakeServer{}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.pinged = true
		log.Warnf("hello fake will return %s", f.resp)
		if f.resp != "" {
			log.Warnf("fake writes %s", f.resp)
			fmt.Fprint(w, f.resp)
		}
	})
	ts := httptest.NewUnstartedServer(foo)
	ts.Listener.Close()
	ts.Listener = l
	ts.Start()

	f.ts = ts

	return &f
}

func (f *FakeServer) Close() {
	f.ts.Close()
}

func (f *FakeServer) PingedAndRestore() bool {
	ret := f.pinged
	f.pinged = false

	return ret
}

type MockMessenger struct {
	lastEvent *Event
}

func (m *MockMessenger) IsConnClosed() bool {
	return false
}

func NewMockMessenger() *MockMessenger {
	return &MockMessenger{nil}
}

func (m *MockMessenger) SendMessage(uuid string, body []byte) error {
	if uuid == "" || body == nil {
		return fmt.Errorf("bad message")
	}

	return nil
}

// nolint:bodyclose
func (suite *ProxyTests) TestServeHTTP_disallowed() {
	proxy := NewProxy(suite.S3Fakeconf, &helper.AlwaysAllow{}, suite.messenger, suite.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	log.Warnf("using proxy on port %d", suite.S3Fakeconf.Port)
	// Remove bucket disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	// Deletion of files are disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/asdf")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	log.Warnf("getting to not allowed stuff")
	// Policy methods are not allowed
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf?acl=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	// Normal get is dissallowed
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	// Put policy is disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf?policy=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	// Create bucket disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 403, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())

	// Not authorized user get 401 response
	proxy = NewProxy(suite.S3Fakeconf, &helper.AlwaysDeny{}, suite.messenger, suite.database, new(tls.Config))
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 401, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())
}

func (suite *ProxyTests) TestServeHTTPS3Unresponsive() {
	s3conf := storage.S3Conf{
		URL:       "http://localhost:40211",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	proxy := NewProxy(s3conf, &helper.AlwaysAllow{}, suite.messenger, suite.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	// Just try to list the files
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/asdf")
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

func (suite *ProxyTests) TestServeHTTP_MQConnectionClosed() {
	// Set up
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	database, _ := database.NewSDAdb(suite.DBConf)
	proxy := NewProxy(suite.S3Fakeconf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// Test that the mq connection will be restored when needed
	proxy.messenger.Connection.Close()
	assert.True(suite.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/connectionclosed-file", nil)
	w := httptest.NewRecorder()
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(suite.T(), proxy.messenger.Connection.IsClosed())
}

func (suite *ProxyTests) TestServeHTTP_MQChannelClosed() {
	// Set up
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	database, _ := database.NewSDAdb(suite.DBConf)
	proxy := NewProxy(suite.S3Fakeconf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// Test that the mq channel will be restored when needed
	proxy.messenger.Channel.Close()
	assert.True(suite.T(), proxy.messenger.Channel.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/channelclosed-file", nil)
	w := httptest.NewRecorder()
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(suite.T(), proxy.messenger.Channel.IsClosed())
}

func (suite *ProxyTests) TestServeHTTP_MQ_Unavailable() {
	// Set up
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	database, _ := database.NewSDAdb(suite.DBConf)
	proxy := NewProxy(suite.S3Fakeconf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// Test that the correct status code is returned when mq connection can't be created
	proxy.messenger.Conf.Port = 123456
	proxy.messenger.Connection.Close()
	assert.True(suite.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/mqunavailable-file", nil)
	w := httptest.NewRecorder()
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

// nolint:bodyclose
func (suite *ProxyTests) TestServeHTTP_allowed() {
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	database, _ := database.NewSDAdb(suite.DBConf)
	proxy := NewProxy(suite.S3Fakeconf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// List files works
	r, err := http.NewRequest("GET", "/dummy/file", nil)
	assert.NoError(suite.T(), err)
	w := httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore()) // Testing the pinged interface

	// Put file works
	w = httptest.NewRecorder()
	r.Method = "PUT"
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Put with partnumber sends no message
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/file?partNumber=5")
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Post with uploadId sends message
	r.Method = "POST"
	r.URL, _ = url.Parse("/dummy/file?uploadId=5")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Post without uploadId sends no message
	r.Method = "POST"
	r.URL, _ = url.Parse("/dummy/file")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Abort multipart works
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/dummy/asdf?uploadId=123")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Going through the different extra stuff that can be in the get request
	// that trigger different code paths in the code.
	// Delimiter alone
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?delimiter=puppe")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Show multiparts uploads
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?uploads")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Delimiter alone together with prefix
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?delimiter=puppe&prefix=asdf")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Location parameter
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?location=fnuffe")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Filenames with platform incompatible characters are disallowed
	// not checked in TestServeHTTP_allowed() because we look for a non 403 response
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/fi|le")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, suite.token)
	assert.Equal(suite.T(), 406, w.Result().StatusCode)
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore())
}

func (suite *ProxyTests) TestMessageFormatting() {
	// Set up basic request for multipart upload
	r, _ := http.NewRequest("POST", "/buckbuck/user/new_file.txt", nil)
	r.Host = "localhost"
	r.Header.Set("content-length", "1234")
	r.Header.Set("x-amz-content-sha256", "checksum")

	claims := jwt.New()
	assert.NoError(suite.T(), claims.Set("sub", "user@host.domain"))

	// start proxy that denies everything
	proxy := NewProxy(suite.S3Fakeconf, &helper.AlwaysDeny{}, suite.messenger, suite.database, new(tls.Config))
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/user/new_file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/user/new_file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>1234</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	msg, err := proxy.CreateMessageFromRequest(r, claims)
	assert.Nil(suite.T(), err)
	assert.IsType(suite.T(), Event{}, msg)

	assert.Equal(suite.T(), int64(1234), msg.Filesize)
	assert.Equal(suite.T(), "user/new_file.txt", msg.Filepath)
	assert.Equal(suite.T(), "user@host.domain", msg.Username)

	c, _ := json.Marshal(msg.Checksum[0])
	checksum := Checksum{}
	_ = json.Unmarshal(c, &checksum)
	assert.Equal(suite.T(), "sha256", checksum.Type)
	assert.Equal(suite.T(), "5b233b981dc12e7ccf4c242b99c042b7842b73b956ad662e4fe0f8354151538b", checksum.Value)

	// Test single shot upload
	r.Method = "PUT"
	msg, err = proxy.CreateMessageFromRequest(r, jwt.New())
	assert.Nil(suite.T(), err)
	assert.IsType(suite.T(), Event{}, msg)
	assert.Equal(suite.T(), "upload", msg.Operation)
}

func (suite *ProxyTests) TestDatabaseConnection() {
	database, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer database.Close()
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer messenger.Connection.Close()
	// Start proxy that allows everything
	proxy := NewProxy(suite.S3Fakeconf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// PUT a file into the system
	filename := "/dummy/db-test-file"

	stringReader := strings.NewReader("a brand new string")
	r, _ := http.NewRequest("PUT", filename, stringReader)
	w := httptest.NewRecorder()
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, suite.token)
	res := w.Result()
	defer res.Body.Close()
	assert.Equal(suite.T(), 200, res.StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Check that the file is registered and uploaded in the database
	// connect to the database
	db, err := sql.Open(suite.DBConf.PgDataSource())
	assert.Nil(suite.T(), err, "Failed to connect to database")

	// Check that the file is in the database
	var fileID string
	query := "SELECT id FROM sda.files WHERE submission_file_path = $1;"
	err = db.QueryRow(query, filename[1:]).Scan(&fileID)
	assert.Nil(suite.T(), err, "Failed to query database")
	assert.NotNil(suite.T(), fileID, "File not found in database")

	// Check that the "registered" status is in the database for this file
	for _, status := range []string{"registered", "uploaded"} {
		var exists int
		query = "SELECT 1 FROM sda.file_event_log WHERE event = $1 AND file_id = $2;"
		err = db.QueryRow(query, status, fileID).Scan(&exists)
		assert.Nil(suite.T(), err, "Failed to find '%v' event in database", status)
		assert.Equal(suite.T(), exists, 1, "File '%v' event does not exist", status)
	}
}

func (suite *ProxyTests) TestFormatUploadFilePath() {
	unixPath := "a/b/c.c4gh"
	testPath := "a\\b\\c.c4gh"
	uploadPath, err := formatUploadFilePath(testPath)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), unixPath, uploadPath)

	// mixed "\" and "/"
	weirdPath := `dq\sw:*?"<>|\t\s/df.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(suite.T(), err, "filepath contains mixed '\\' and '/' characters")

	// no mixed "\" and "/" but not allowed
	weirdPath = `dq\sw:*?"<>|\t\sdf!s'(a);w@4&f=+e$,g#[]d%.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(suite.T(), err, "filepath contains disallowed characters: :, *, ?, \", <, >, |, !, ', (, ), ;, @, &, =, +, $, ,, #, [, ], %")
}

func (suite *ProxyTests) TestCheckFileExists() {
	database, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer database.Close()
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer messenger.Connection.Close()
	proxy := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), messenger, database, new(tls.Config))

	res, err := proxy.checkFileExists("/dummy/file")
	assert.True(suite.T(), res)
	assert.Nil(suite.T(), err)
}

func (suite *ProxyTests) TestCheckFileExists_nonExistingFile() {
	// Check that looking for a non-existing file gives (false, nil)
	// from checkFileExists
	database, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer database.Close()
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer messenger.Connection.Close()
	proxy := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), suite.messenger, suite.database, new(tls.Config))

	res, err := proxy.checkFileExists("nonexistingfilepath")
	assert.False(suite.T(), res)
	assert.Nil(suite.T(), err)
}

func (suite *ProxyTests) TestCheckFileExists_unresponsive() {
	// Check that errors when connecting to S3 are forwarded
	// and that checkFileExists return (false, someError)
	database, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer database.Close()
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer messenger.Connection.Close()

	// Unaccessible S3 (wrong port)
	proxy := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), suite.messenger, suite.database, new(tls.Config))
	proxy.s3.Port = 1111

	res, err := proxy.checkFileExists("nonexistingfilepath")
	assert.False(suite.T(), res)
	assert.NotNil(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "S3: HeadObject")

	// Bad access key gives 403
	proxy.s3.Port = suite.S3conf.Port
	proxy.s3.AccessKey = "invaild"
	res, err = proxy.checkFileExists("nonexistingfilepath")
	assert.False(suite.T(), res)
	assert.NotNil(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "StatusCode: 403")
}

func (suite *ProxyTests) TestStoreObjectSizeInDB() {
	db, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer db.Close()

	mq, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer mq.Connection.Close()

	p := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), suite.messenger, suite.database, new(tls.Config))
	p.database = db

	fileID, err := db.RegisterFile("/dummy/file", "test-user")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), fileID)

	assert.NoError(suite.T(), p.storeObjectSizeInDB("/dummy/file", fileID))

	const getObjectSize = "SELECT submission_file_size FROM sda.files WHERE id = $1;"
	var objectSize int64
	assert.NoError(suite.T(), p.database.DB.QueryRow(getObjectSize, fileID).Scan(&objectSize))
	assert.Equal(suite.T(), int64(14), objectSize)
}

func (suite *ProxyTests) TestStoreObjectSizeInDB_dbFailure() {
	db, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)

	mq, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer mq.Connection.Close()

	p := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), suite.messenger, suite.database, new(tls.Config))
	p.database = db

	fileID, err := db.RegisterFile("/dummy/file", "test-user")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), fileID)

	db.Close()
	assert.NoError(suite.T(), p.storeObjectSizeInDB("/dummy/file", fileID))
}

func (suite *ProxyTests) TestStoreObjectSizeInDB_s3Failure() {
	db, err := database.NewSDAdb(suite.DBConf)
	assert.NoError(suite.T(), err)
	defer db.Close()
	mq, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	defer mq.Connection.Close()

	p := NewProxy(suite.S3conf, helper.NewAlwaysAllow(), suite.messenger, suite.database, new(tls.Config))
	p.database = db

	fileID, err := db.RegisterFile("/dummy/file", "test-user")
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), fileID)

	p.s3.AccessKey = "badKey"
	assert.Error(suite.T(), p.storeObjectSizeInDB("/dummy/file", fileID))

	p.s3.Port = 1234
	assert.Error(suite.T(), p.storeObjectSizeInDB("/dummy/file", fileID))
}
