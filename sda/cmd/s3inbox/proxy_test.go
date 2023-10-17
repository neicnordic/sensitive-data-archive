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
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
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
}

func TestProxyTestSuite(t *testing.T) {
	suite.Run(t, new(ProxyTests))
}

func (suite *ProxyTests) SetupTest() {

	// Create fake server
	suite.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	suite.S3conf = storage.S3Conf{
		URL:       "http://127.0.0.1",
		Port:      9024,
		AccessKey: "someAccess",
		SecretKey: "someSecret",
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
		Exchange: "amq.default",
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
	f := FakeServer{}
	foo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.pinged = true
		fmt.Fprintf(w, f.resp)
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
	// Start mock messenger that denies everything
	proxy := NewProxy(suite.S3conf, &userauth.AlwaysDeny{}, suite.messenger, suite.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

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
	proxy := NewProxy(s3conf, &userauth.AlwaysAllow{}, suite.messenger, suite.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	// Just try to list the files
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf/asdf")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

// nolint:bodyclose
func (suite *ProxyTests) TestServeHTTP_allowed() {

	// Start proxy that allows everything
	messenger, err := broker.NewMQ(suite.MQConf)
	assert.NoError(suite.T(), err)
	database, _ := database.NewSDAdb(suite.DBConf)
	proxy := NewProxy(suite.S3conf, userauth.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// List files works
	r, err := http.NewRequest("GET", "/username/file", nil)
	assert.NoError(suite.T(), err)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())
	assert.Equal(suite.T(), false, suite.fakeServer.PingedAndRestore()) // Testing the pinged interface

	// Put file works
	w = httptest.NewRecorder()
	r.Method = "PUT"
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	assert.False(suite.T(), messenger.IsConnClosed())
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Put with partnumber sends no message
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/username/file?partNumber=5")
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Post with uploadId sends message
	r.Method = "POST"
	r.URL, _ = url.Parse("/username/file?uploadId=5")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Post without uploadId sends no message
	r.Method = "POST"
	r.URL, _ = url.Parse("/username/file")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Abort multipart works
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/asdf?uploadId=123")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Going through the different extra stuff that can be in the get request
	// that trigger different code paths in the code.
	// Delimiter alone
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file?delimiter=puppe")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Show multiparts uploads
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file?uploads")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Delimiter alone together with prefix
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file?delimiter=puppe&prefix=asdf")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Location parameter
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file?location=fnuffe")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(suite.T(), 200, w.Result().StatusCode)
	assert.Equal(suite.T(), true, suite.fakeServer.PingedAndRestore())

	// Filenames with platform incompatible characters are disallowed
	// not checked in TestServeHTTP_allowed() because we look for a non 403 response
	r.Method = "PUT"
	r.URL, _ = url.Parse("/username/fi|le")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
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
	proxy := NewProxy(suite.S3conf, &userauth.AlwaysDeny{}, suite.messenger, suite.database, new(tls.Config))
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
	proxy := NewProxy(suite.S3conf, userauth.NewAlwaysAllow(), messenger, database, new(tls.Config))

	// PUT a file into the system
	filename := "/username/db-test-file"
	r, _ := http.NewRequest("PUT", filename, nil)
	w := httptest.NewRecorder()
	suite.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	proxy.ServeHTTP(w, r)
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
	query := "SELECT id FROM sda.files WHERE submission_file_path = $1"
	err = db.QueryRow(query, filename[1:]).Scan(&fileID)
	assert.Nil(suite.T(), err, "Failed to query database")
	assert.NotNil(suite.T(), fileID, "File not found in database")

	// Check that the "registered" status is in the database for this file
	for _, status := range []string{"registered", "uploaded"} {
		var exists int
		query = "SELECT 1 FROM sda.file_event_log WHERE event = $1 AND file_id = $2"
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
	weirdPath = `dq\sw:*?"<>|\t\sdf.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(suite.T(), err, "filepath contains disallowed characters: :, *, ?, \", <, >, |")
}
