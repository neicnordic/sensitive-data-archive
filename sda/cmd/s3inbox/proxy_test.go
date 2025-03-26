package main

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ProxyTests struct {
	suite.Suite
	S3conf     storage.S3Conf
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

func (pt *ProxyTests) SetupTest() {
	// Create fake server
	pt.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	pt.S3conf = storage.S3Conf{
		URL:       "http://127.0.0.1",
		Port:      9024,
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}

	// Create a configuration for the fake MQ
	pt.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     MQport,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	pt.messenger = &broker.AMQPBroker{}

	// Create a database configuration for the fake database
	pt.DBConf = database.DBConf{
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

	pt.database = &database.SDAdb{}

	// Create temp demo rsa key pair
	demoKeysPath := "demo-rsa-keys"
	defer os.RemoveAll(demoKeysPath)
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(pt.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(pt.T(), err)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateRSAKey(prKeyPath, "/rsa")
	assert.NoError(pt.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(pt.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"
	pt.auth = userauth.NewValidateFromToken(jwk.NewSet())
	_ = pt.auth.ReadJwtPubKeyPath(jwtpubkeypath)

	pt.token, err = jwt.Parse([]byte(defaultToken), jwt.WithKeySet(pt.auth.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	if err != nil {
		pt.T().FailNow()
	}
}

func (pt *ProxyTests) TearDownTest() {
	pt.fakeServer.Close()
	pt.database.Close()
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
	f := FakeServer{}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.pinged = true
		fmt.Fprint(w, f.resp)
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
func (pt *ProxyTests) TestServeHTTP_disallowed() {
	proxy := NewProxy(pt.S3conf, &helper.AlwaysAllow{}, pt.messenger, pt.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	// Remove bucket disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Deletion of files are disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/asdf")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Policy methods are not allowed
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf?acl=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Normal get is dissallowed
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Put policy is disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf?policy=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Create bucket disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 403, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())

	// Not authorized user get 401 response
	proxy = NewProxy(pt.S3conf, &helper.AlwaysDeny{}, pt.messenger, pt.database, new(tls.Config))
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file")
	proxy.ServeHTTP(w, r)
	assert.Equal(pt.T(), 401, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())
}

func (pt *ProxyTests) TestServeHTTPS3Unresponsive() {
	s3conf := storage.S3Conf{
		URL:       "http://localhost:40211",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	proxy := NewProxy(s3conf, &helper.AlwaysAllow{}, pt.messenger, pt.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	// Just try to list the files
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/asdf")
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

func (pt *ProxyTests) TestServeHTTP_MQConnectionClosed() {
	// Set up
	messenger, err := broker.NewMQ(pt.MQConf)
	assert.NoError(pt.T(), err)
	db, _ := database.NewSDAdb(pt.DBConf)
	proxy := NewProxy(pt.S3conf, helper.NewAlwaysAllow(), messenger, db, new(tls.Config))

	// Test that the mq connection will be restored when needed
	proxy.messenger.Connection.Close()
	assert.True(pt.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/connectionclosed-file", nil)
	w := httptest.NewRecorder()
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(pt.T(), proxy.messenger.Connection.IsClosed())
}

func (pt *ProxyTests) TestServeHTTP_MQChannelClosed() {
	// Set up
	messenger, err := broker.NewMQ(pt.MQConf)
	assert.NoError(pt.T(), err)
	db, _ := database.NewSDAdb(pt.DBConf)
	proxy := NewProxy(pt.S3conf, helper.NewAlwaysAllow(), messenger, db, new(tls.Config))

	// Test that the mq channel will be restored when needed
	proxy.messenger.Channel.Close()
	assert.True(pt.T(), proxy.messenger.Channel.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/channelclosed-file", nil)
	w := httptest.NewRecorder()
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(pt.T(), proxy.messenger.Channel.IsClosed())
}

func (pt *ProxyTests) TestServeHTTP_MQ_Unavailable() {
	// Set up
	messenger, err := broker.NewMQ(pt.MQConf)
	assert.NoError(pt.T(), err)
	db, _ := database.NewSDAdb(pt.DBConf)
	proxy := NewProxy(pt.S3conf, helper.NewAlwaysAllow(), messenger, db, new(tls.Config))

	// Test that the correct status code is returned when mq connection can't be created
	proxy.messenger.Conf.Port = 123456
	proxy.messenger.Connection.Close()
	assert.True(pt.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/mqunavailable-file", nil)
	w := httptest.NewRecorder()
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

// nolint:bodyclose
func (pt *ProxyTests) TestServeHTTP_allowed() {
	messenger, err := broker.NewMQ(pt.MQConf)
	assert.NoError(pt.T(), err)
	db, _ := database.NewSDAdb(pt.DBConf)
	proxy := NewProxy(pt.S3conf, helper.NewAlwaysAllow(), messenger, db, new(tls.Config))

	// List files works
	r, err := http.NewRequest("GET", "/dummy/file", nil)
	assert.NoError(pt.T(), err)
	w := httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore()) // Testing the pinged interface

	// Put file works
	w = httptest.NewRecorder()
	r.Method = "PUT"
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Put with partnumber sends no message
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/file?partNumber=5")
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Post with uploadId sends message
	r.Method = "POST"
	r.URL, _ = url.Parse("/dummy/file?uploadId=5")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Post without uploadId sends no message
	r.Method = "POST"
	r.URL, _ = url.Parse("/dummy/file")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Abort multipart works
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/dummy/asdf?uploadId=123")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Going through the different extra stuff that can be in the get request
	// that trigger different code paths in the code.
	// Delimiter alone
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?delimiter=puppe")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Show multiparts uploads
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?uploads")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Delimiter alone together with prefix
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?delimiter=puppe&prefix=asdf")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Location parameter
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?location=fnuffe")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 200, w.Result().StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Filenames with platform incompatible characters are disallowed
	// not checked in TestServeHTTP_allowed() because we look for a non 403 response
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/fi|le")
	w = httptest.NewRecorder()
	proxy.allowedResponse(w, r, pt.token)
	assert.Equal(pt.T(), 406, w.Result().StatusCode)
	assert.Equal(pt.T(), false, pt.fakeServer.PingedAndRestore())
}

func (pt *ProxyTests) TestMessageFormatting() {
	// Set up basic request for multipart upload
	r, _ := http.NewRequest("POST", "/buckbuck/user/new_file.txt", nil)
	r.Host = "localhost"
	r.Header.Set("content-length", "1234")
	r.Header.Set("x-amz-content-sha256", "checksum")

	claims := jwt.New()
	assert.NoError(pt.T(), claims.Set("sub", "user@host.domain"))

	// start proxy that denies everything
	proxy := NewProxy(pt.S3conf, &helper.AlwaysDeny{}, pt.messenger, pt.database, new(tls.Config))
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/user/new_file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/user/new_file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>1234</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	msg, err := proxy.CreateMessageFromRequest(r, claims)
	assert.Nil(pt.T(), err)
	assert.IsType(pt.T(), Event{}, msg)

	assert.Equal(pt.T(), int64(1234), msg.Filesize)
	assert.Equal(pt.T(), "user/new_file.txt", msg.Filepath)
	assert.Equal(pt.T(), "user@host.domain", msg.Username)

	c, _ := json.Marshal(msg.Checksum[0])
	checksum := Checksum{}
	_ = json.Unmarshal(c, &checksum)
	assert.Equal(pt.T(), "sha256", checksum.Type)
	assert.Equal(pt.T(), "5b233b981dc12e7ccf4c242b99c042b7842b73b956ad662e4fe0f8354151538b", checksum.Value)

	// Test single shot upload
	r.Method = "PUT"
	msg, err = proxy.CreateMessageFromRequest(r, jwt.New())
	assert.Nil(pt.T(), err)
	assert.IsType(pt.T(), Event{}, msg)
	assert.Equal(pt.T(), "upload", msg.Operation)
}

func (pt *ProxyTests) TestDatabaseConnection() {
	sdadb, err := database.NewSDAdb(pt.DBConf)
	assert.NoError(pt.T(), err)
	defer sdadb.Close()
	messenger, err := broker.NewMQ(pt.MQConf)
	assert.NoError(pt.T(), err)
	defer messenger.Connection.Close()
	// Start proxy that allows everything
	proxy := NewProxy(pt.S3conf, helper.NewAlwaysAllow(), messenger, sdadb, new(tls.Config))

	// PUT a file into the system
	filename := "/dummy/db-test-file"
	r, _ := http.NewRequest("PUT", filename, nil)
	w := httptest.NewRecorder()
	pt.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.allowedResponse(w, r, pt.token)
	res := w.Result()
	defer res.Body.Close()
	assert.Equal(pt.T(), 200, res.StatusCode)
	assert.Equal(pt.T(), true, pt.fakeServer.PingedAndRestore())

	// Check that the file is registered and uploaded in the database
	// connect to the database
	db, err := sql.Open(pt.DBConf.PgDataSource())
	assert.Nil(pt.T(), err, "Failed to connect to database")

	// Check that the file is in the database
	var fileID string
	query := "SELECT id FROM sda.files WHERE submission_file_path = $1;"
	err = db.QueryRow(query, filename[1:]).Scan(&fileID)
	assert.Nil(pt.T(), err, "Failed to query database")
	assert.NotNil(pt.T(), fileID, "File not found in database")

	// Check that the "registered" status is in the database for this file
	for _, status := range []string{"registered", "uploaded"} {
		var exists int
		query = "SELECT 1 FROM sda.file_event_log WHERE event = $1 AND file_id = $2;"
		err = db.QueryRow(query, status, fileID).Scan(&exists)
		assert.Nil(pt.T(), err, "Failed to find '%v' event in database", status)
		assert.Equal(pt.T(), exists, 1, "File '%v' event does not exist", status)
	}
}

func (pt *ProxyTests) TestFormatUploadFilePath() {
	unixPath := "a/b/c.c4gh"
	testPath := "a\\b\\c.c4gh"
	uploadPath, err := formatUploadFilePath(testPath)
	assert.NoError(pt.T(), err)
	assert.Equal(pt.T(), unixPath, uploadPath)

	// mixed "\" and "/"
	weirdPath := `dq\sw:*?"<>|\t\s/df.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(pt.T(), err, "filepath contains mixed '\\' and '/' characters")

	// no mixed "\" and "/" but not allowed
	weirdPath = `dq\sw:*?"<>|\t\sdf!s'(a);w@4&f=+e$,g#[]d%.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(pt.T(), err, "filepath contains disallowed characters: :, *, ?, \", <, >, |, !, ', (, ), ;, @, &, =, +, $, ,, #, [, ], %")
}
