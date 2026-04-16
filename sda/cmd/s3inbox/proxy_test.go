package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/userauth"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ProxyTests struct {
	suite.Suite
	s3Fakeconf     config.S3InboxConf // fakeserver
	s3ClientToFake *s3.Client
	s3Conf         config.S3InboxConf // actual s3 container
	s3Client       *s3.Client

	fakeServer     *FakeServer
	MQConf         broker.MQConf
	messenger      *broker.AMQPBroker
	database       database.Database
	verificationDB *sql.DB

	auth  *userauth.ValidateFromToken
	token jwt.Token
}

func TestProxyTestSuite(t *testing.T) {
	suite.Run(t, new(ProxyTests))
}

func (s *ProxyTests) SetupTest() {
	// Create fake server
	s.fakeServer = startFakeServer("9024")

	// Create an s3config for the fake server
	s.s3Fakeconf = config.S3InboxConf{
		Endpoint:  "http://127.0.0.1:9024",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	var err error
	s.s3ClientToFake, err = newS3Client(context.TODO(), s.s3Fakeconf)
	if err != nil {
		s.FailNow(err.Error())
	}

	s.s3Conf = config.S3InboxConf{
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d", s3Port),
		AccessKey: "access",
		SecretKey: "secretKey",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	s.s3Client, err = newS3Client(context.TODO(), s.s3Conf)
	if err != nil {
		s.FailNow(err.Error())
	}

	// Create a configuration for the fake MQ
	s.MQConf = broker.MQConf{
		Host:     "127.0.0.1",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Vhost:    "/",
		Exchange: "",
	}

	s.messenger = &broker.AMQPBroker{}

	// Create a database configuration for the fake database
	s.database, err = postgres.NewPostgresSQLDatabase(
		postgres.Host("127.0.0.1"),
		postgres.Port(dbPort),
		postgres.User("postgres"),
		postgres.Password("rootpasswd"),
		postgres.DatabaseName("sda"),
		postgres.Schema("sda"),
		postgres.CACert(""),
		postgres.SslMode("disable"),
		postgres.ClientCert(""),
		postgres.ClientKey(""),
	)
	if err != nil {
		s.FailNow("failed to connect to database", err)
	}

	s.verificationDB, err = sql.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%d user=postgres password=rootpasswd dbname=sda sslmode=disable search_path=sda", dbPort))
	if err != nil {
		s.FailNow(fmt.Sprintf("failed to connect to database: %v", err))
	}

	// Create temp demo rsa key pair
	demoKeysPath := "demo-rsa-keys"
	defer os.RemoveAll(demoKeysPath)
	prKeyPath, pubKeyPath, err := helper.MakeFolder(demoKeysPath)
	assert.NoError(s.T(), err)

	err = helper.CreateRSAkeys(prKeyPath, pubKeyPath)
	assert.NoError(s.T(), err)

	// Parse demo private key
	prKeyParsed, err := helper.ParsePrivateRSAKey(prKeyPath, "/rsa")
	assert.NoError(s.T(), err)

	// Create token and set up request defaults
	defaultToken, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(s.T(), err)

	jwtpubkeypath := demoKeysPath + "/public-key/"
	s.auth = userauth.NewValidateFromToken(jwk.NewSet())
	_ = s.auth.ReadJwtPubKeyPath(jwtpubkeypath)

	s.token, err = jwt.Parse([]byte(defaultToken), jwt.WithKeySet(s.auth.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
	if err != nil {
		s.T().FailNow()
	}
	if err := s.token.Set("sub", "dummy"); err != nil {
		s.T().FailNow()
	}

	s3cfg, err := s3config.LoadDefaultConfig(context.TODO(), s3config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.s3Conf.AccessKey, s.s3Conf.SecretKey, "")))
	if err != nil {
		s.FailNow("bad")
	}
	s3Client := s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s.s3Conf.Endpoint)
			o.EndpointOptions.DisableHTTPS = strings.HasPrefix(s.s3Conf.Endpoint, "http:")
			o.Region = s.s3Conf.Region
			o.UsePathStyle = true
		},
	)
	_, _ = s3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{Bucket: aws.String(s.s3Conf.Bucket)})
	if err != nil {
		_, _ = fmt.Println(err.Error())
	}

	output, err := s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Body:            strings.NewReader("This is a test"),
		Bucket:          aws.String(s.s3Conf.Bucket),
		Key:             aws.String("/dummy/file"),
		ContentEncoding: aws.String("application/octet-stream"),
	})
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), output, output)
}

func (s *ProxyTests) TearDownTest() {
	s.fakeServer.Close()
	_ = s.database.Close()
	_ = s.verificationDB.Close()
}

// TestServeHTTP_concurrent_put_noRace verifies that after the in-memory
// fileIDs map was replaced with Postgres-backed lookups (#2382), concurrent
// PUT requests no longer trigger the "fatal error: concurrent map writes"
// crash from #2295. The same-shaped test fails on main under -race with
// warnings on proxy.go:182/185/195 and the fatal error. Here it should pass.
func (s *ProxyTests) TestServeHTTP_concurrent_put_noRace() {
	const workers = 50
	const rounds = 20

	proxy := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), nil, s.database, new(tls.Config))
	proxy.s3Conf.Endpoint = ":" // force forwardRequestToBackend to fail fast

	for round := 0; round < rounds; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			n := round*workers + i
			go func(n int) {
				defer wg.Done()
				req := httptest.NewRequest(
					http.MethodPut,
					fmt.Sprintf("/dummy/race-%04d.c4gh", n),
					http.NoBody,
				)
				rec := httptest.NewRecorder()
				<-start
				proxy.ServeHTTP(rec, req)
			}(n)
		}
		close(start)
		wg.Wait()
	}
}

type FakeServer struct {
	ts          *httptest.Server
	headHeaders map[string]string
	resp        string
	pinged      bool
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
		if f.headHeaders != nil && r.Method == "HEAD" {
			for k, v := range f.headHeaders {
				w.Header().Set(k, v)
			}
			w.WriteHeader(http.StatusOK)

			return
		}
		if f.resp != "" {
			_, _ = fmt.Fprint(w, f.resp)
		}
	})
	ts := httptest.NewUnstartedServer(foo)
	_ = ts.Listener.Close()
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
		return errors.New("bad message")
	}

	return nil
}

// nolint:bodyclose
func (s *ProxyTests) TestServeHTTP_disallowed() {
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, &helper.AlwaysAllow{}, s.messenger, s.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	log.Warnf("using proxy at %s", s.s3Fakeconf.Endpoint)
	// Remove bucket disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 403, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	// Deletion of files are disallowed
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/asdf/asdf")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 403, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	log.Warnf("getting to not allowed stuff")
	// Policy methods are not allowed
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/asdf?acl=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 403, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	// Put policy is disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf?policy=rw")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 403, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	// Create bucket disallowed
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/asdf/")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 403, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	// Not authorized user get 401 response
	proxy = NewProxy(s.s3Fakeconf, s.s3ClientToFake, &helper.AlwaysDeny{}, s.messenger, s.database, new(tls.Config))
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/username/file")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 401, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())
}

func (s *ProxyTests) TestServeHTTPS3Unresponsive() {
	s3conf := config.S3InboxConf{
		Endpoint:  "http://localhost:40211",
		AccessKey: "someAccess",
		SecretKey: "someSecret",
		Bucket:    "buckbuck",
		Region:    "us-east-1",
	}
	proxy := NewProxy(s3conf, s.s3Client, &helper.AlwaysAllow{}, s.messenger, s.database, new(tls.Config))

	r, _ := http.NewRequest("", "", nil)
	w := httptest.NewRecorder()

	// Just try to list the files
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

func (s *ProxyTests) TestServeHTTP_MQConnectionClosed() {
	// Set up
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))

	// Test that the mq connection will be restored when needed
	proxy.messenger.Connection.Close()
	assert.True(s.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/connectionclosed-file", nil)
	w := httptest.NewRecorder()
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\"", "Content-Length": "5"}
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(s.T(), proxy.messenger.Connection.IsClosed())
}

func (s *ProxyTests) TestServeHTTP_MQChannelClosed() {
	// Set up
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))

	// Test that the mq channel will be restored when needed
	proxy.messenger.Channel.Close()
	assert.True(s.T(), proxy.messenger.Channel.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/channelclosed-file", nil)
	w := httptest.NewRecorder()
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\"", "Content-Length": "5"}
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode) // nolint:bodyclose
	assert.False(s.T(), proxy.messenger.Channel.IsClosed())
}

func (s *ProxyTests) TestServeHTTP_MQ_Unavailable() {
	// Set up
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))

	// Test that the correct status code is returned when mq connection can't be created
	proxy.messenger.Conf.Port = 123456
	proxy.messenger.Connection.Close()
	assert.True(s.T(), proxy.messenger.Connection.IsClosed())
	r, _ := http.NewRequest("PUT", "/dummy/mqunavailable-file", nil)
	w := httptest.NewRecorder()
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\"", "Content-Length": "5"}
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 500, w.Result().StatusCode) // nolint:bodyclose
}

// nolint:bodyclose
func (s *ProxyTests) TestServeHTTP_allowed() {
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))

	// List files works
	r, err := http.NewRequest("GET", "/dummy", nil)
	assert.NoError(s.T(), err)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore()) // Testing the pinged interface

	// Put file works
	w = httptest.NewRecorder()
	r, err = http.NewRequest("PUT", "/dummy/file", nil)
	assert.NoError(s.T(), err)
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\";", "Content-Length": "5"}
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Put with partnumber sends no message
	w = httptest.NewRecorder()
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/file?partNumber=5&uploadId=1")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Post with uploadId sends message
	r.Method = "POST"
	r.URL, _ = url.Parse("/dummy/file?uploadId=5")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Abort multipart works
	r.Method = "DELETE"
	r.URL, _ = url.Parse("/dummy/asdf?uploadId=123")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Going through the different extra stuff that can be in the get request
	// that trigger different code paths in the code.
	// Delimiter alone
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy?delimiter=puppe")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Show multiparts uploads
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy/file?uploadId=1")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Delimiter alone together with prefix
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy?delimiter=puppe&prefix=asdf")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Location parameter
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy?location=fnuffe")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Filenames with platform incompatible characters are disallowed
	// not checked in TestServeHTTP_allowed() because we look for a non 403 response
	r.Method = "PUT"
	r.URL, _ = url.Parse("/dummy/fi|le")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 400, w.Result().StatusCode)
	assert.Equal(s.T(), false, s.fakeServer.PingedAndRestore())

	// Get with list-type=2
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy?list-type=2")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Get with uploads
	w = httptest.NewRecorder()
	r.Method = "GET"
	r.URL, _ = url.Parse("/dummy?uploads")
	proxy.ServeHTTP(w, r)
	assert.Equal(s.T(), 200, w.Result().StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())
}

func (s *ProxyTests) TestMessageFormatting() {
	// Set up basic request for multipart upload
	r, _ := http.NewRequest("POST", "/buckbuck/user/new_file.txt", nil)
	r.Host = "localhost"
	r.Header.Set("content-length", "1234")
	r.Header.Set("x-amz-content-sha256", "checksum")

	claims := jwt.New()
	user := "user@host.domain"
	assert.NoError(s.T(), claims.Set("sub", user))

	// start proxy that denies everything
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, &helper.AlwaysDeny{}, s.messenger, s.database, new(tls.Config))
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/user/new_file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/user/new_file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>1234</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\"", "Content-Length": "1234"}
	msg, checksumValue, err := proxy.CreateMessageFromRequest(r.Context(), claims.Subject(), "new_file.txt")
	assert.Nil(s.T(), err)
	assert.IsType(s.T(), Event{}, msg)

	assert.Equal(s.T(), int64(1234), msg.Filesize)
	assert.Equal(s.T(), "new_file.txt", msg.Filepath)
	assert.Equal(s.T(), "user@host.domain", msg.Username)

	c, _ := json.Marshal(msg.Checksum[0])
	checksum := Checksum{}
	_ = json.Unmarshal(c, &checksum)
	assert.Equal(s.T(), "md5", checksum.Type)
	assert.Equal(s.T(), "0a44282bd39178db9680f24813c41aec-1", checksum.Value)
	assert.Equal(s.T(), "0a44282bd39178db9680f24813c41aec-1", checksumValue)

	// Test single shot upload
	r.Method = "PUT"
	msg, _, err = proxy.CreateMessageFromRequest(r.Context(), claims.Subject(), "new_file.txt")
	assert.Nil(s.T(), err)
	assert.IsType(s.T(), Event{}, msg)
	assert.Equal(s.T(), "upload", msg.Operation)
	assert.Equal(s.T(), "new_file.txt", msg.Filepath)
}

func (s *ProxyTests) TestDatabaseConnection() {
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer messenger.Connection.Close()
	// Start proxy that allows everything
	proxy := NewProxy(s.s3Fakeconf, s.s3ClientToFake, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))

	// PUT a file into the system
	filename := "/dummy/db-test-file"
	anonymFilename := "db-test-file"
	stringReader := strings.NewReader("a brand new string")
	r, _ := http.NewRequest("PUT", filename, stringReader)
	w := httptest.NewRecorder()
	s.fakeServer.resp = "<ListBucketResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><Name>test</Name><Prefix>/elixirid/db-test-file.txt</Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter></Delimiter><IsTruncated>false</IsTruncated><Contents><Key>/elixirid/file.txt</Key><LastModified>2020-03-10T13:20:15.000Z</LastModified><ETag>&#34;0a44282bd39178db9680f24813c41aec-1&#34;</ETag><Size>5</Size><Owner><ID></ID><DisplayName></DisplayName></Owner><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>"
	s.fakeServer.headHeaders = map[string]string{"ETag": "\"0a44282bd39178db9680f24813c41aec-1\"", "Content-Length": "5"}
	proxy.ServeHTTP(w, r)
	res := w.Result()
	defer res.Body.Close()
	assert.Equal(s.T(), 200, res.StatusCode)
	assert.Equal(s.T(), true, s.fakeServer.PingedAndRestore())

	// Check that the file is in the database
	var fileID, location string
	query := "SELECT id, submission_location FROM sda.files WHERE submission_file_path = $1;"
	err = s.verificationDB.QueryRow(query, anonymFilename).Scan(&fileID, &location)
	assert.Nil(s.T(), err, "Failed to query database")
	assert.NotNil(s.T(), fileID, "File not found in database")
	assert.Equal(s.T(), fmt.Sprintf("%s/%s", s.s3Fakeconf.Endpoint, s.s3Fakeconf.Bucket), location)

	// Check that the "registered" status is in the database for this file
	for _, status := range []string{"registered", "uploaded"} {
		var exists int
		query = "SELECT 1 FROM sda.file_event_log WHERE event = $1 AND file_id = $2;"
		err = s.verificationDB.QueryRow(query, status, fileID).Scan(&exists)
		assert.Nil(s.T(), err, "Failed to find '%v' event in database", status)
		assert.Equal(s.T(), exists, 1, "File '%v' event does not exist", status)
	}
}

func (s *ProxyTests) TestFormatUploadFilePath() {
	unixPath := "a/b/c.c4gh"
	testPath := "a\\b\\c.c4gh"
	uploadPath, err := formatUploadFilePath(testPath)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), unixPath, uploadPath)

	// mixed "\" and "/"
	weirdPath := `dq\sw:*?"<>|\t\s/df.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(s.T(), err, "filepath contains mixed '\\' and '/' characters")

	// no mixed "\" and "/" but not allowed
	weirdPath = `dq\sw:*?"<>|\t\sdf!s'(a);w@4&f=+e$,g#[]d%.c4gh`
	_, err = formatUploadFilePath(weirdPath)
	assert.EqualError(s.T(), err, "filepath contains disallowed characters: :, *, ?, \", <, >, |, !, ', (, ), ;, @, &, =, +, $, ,, #, [, ], %")
}

func (s *ProxyTests) TestCheckFileExists() {
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer messenger.Connection.Close()
	proxy := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), messenger, s.database, new(tls.Config))
	res, err := proxy.checkFileExists(context.TODO(), "/dummy/file")
	assert.True(s.T(), res)
	assert.Nil(s.T(), err)
}

func (s *ProxyTests) TestCheckFileExists_nonExistingFile() {
	// Check that looking for a non-existing file gives (false, nil)
	// from checkFileExists
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer messenger.Connection.Close()
	proxy := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), s.messenger, s.database, new(tls.Config))
	res, err := proxy.checkFileExists(context.TODO(), "nonexistingfilepath")
	assert.False(s.T(), res)
	assert.Nil(s.T(), err)
}

func (s *ProxyTests) TestCheckFileExists_unresponsive() {
	// Check that errors when connecting to S3 are forwarded
	// and that checkFileExists return (false, someError)
	messenger, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer messenger.Connection.Close()

	// Unaccessible S3 (wrong port)
	proxy := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), s.messenger, s.database, new(tls.Config))
	proxy.s3Conf.Endpoint = "http://127.0.0.1:1111"
	proxy.s3Client, err = newS3Client(context.TODO(), proxy.s3Conf)
	assert.NoError(s.T(), err)
	res, err := proxy.checkFileExists(context.TODO(), "nonexistingfilepath")
	assert.False(s.T(), res)
	assert.NotNil(s.T(), err)
	assert.Contains(s.T(), err.Error(), "S3: HeadObject")

	// Bad access key gives 403
	proxy.s3Conf.Endpoint = s.s3Conf.Endpoint
	proxy.s3Conf.AccessKey = "invaild"
	proxy.s3Client, err = newS3Client(context.TODO(), proxy.s3Conf)
	assert.NoError(s.T(), err)
	res, err = proxy.checkFileExists(context.TODO(), "nonexistingfilepath")
	assert.False(s.T(), res)
	assert.NotNil(s.T(), err)
	assert.Contains(s.T(), err.Error(), "StatusCode: 403")
}

func (s *ProxyTests) TestStoreObjectSizeInDB_s3Failure() {
	mq, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer mq.Connection.Close()

	proxy := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), s.messenger, s.database, new(tls.Config))

	fileID, err := proxy.database.RegisterFile(context.TODO(), nil, "/inbox", "/dummy/file", "test-user")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), fileID)

	// Detect autentication failure
	proxy.s3Conf.AccessKey = "badKey"
	proxy.s3Client, err = newS3Client(context.TODO(), proxy.s3Conf)
	assert.NoError(s.T(), err)
	assert.Error(s.T(), proxy.storeObjectSizeInDB(context.TODO(), "/dummy/file", fileID))

	// Detect unresponsive backend service
	proxy.s3Conf.Endpoint = "http://127.0.0.1:1234"
	proxy.s3Client, err = newS3Client(context.TODO(), proxy.s3Conf)
	assert.NoError(s.T(), err)
	assert.Error(s.T(), proxy.storeObjectSizeInDB(context.TODO(), "/dummy/file", fileID))
}

// This test is intended to try to catch some issues we sometimes see when a query to the S3 backend
// happens to fast so that it is not ready and returns a false 404.
func (s *ProxyTests) TestStoreObjectSizeInDB_fastCheck() {
	mq, err := broker.NewMQ(s.MQConf)
	assert.NoError(s.T(), err)
	defer mq.Connection.Close()

	p := NewProxy(s.s3Conf, s.s3Client, helper.NewAlwaysAllow(), s.messenger, s.database, new(tls.Config))

	fileID, err := p.database.RegisterFile(context.TODO(), nil, "/inbox", "/test/new_file", "test-user")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), fileID)

	s3cfg, err := s3config.LoadDefaultConfig(context.TODO(), s3config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.s3Conf.AccessKey, s.s3Conf.SecretKey, "")))
	if err != nil {
		s.FailNow(err.Error())
	}
	s3Client := s3.NewFromConfig(
		s3cfg,
		func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s.s3Conf.Endpoint)
			o.EndpointOptions.DisableHTTPS = strings.HasPrefix(s.s3Conf.Endpoint, "http:")
			o.Region = s.s3Conf.Region
			o.UsePathStyle = true
		},
	)

	output, err := s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Body:            strings.NewReader(strings.Repeat("A", 10*1024*1024)),
		Bucket:          aws.String(s.s3Conf.Bucket),
		Key:             aws.String("/test/new_file"),
		ContentEncoding: aws.String("application/octet-stream"),
	})
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), output, output)

	assert.NoError(s.T(), p.storeObjectSizeInDB(context.TODO(), "/test/new_file", fileID))

	var objectSize int64
	// If the S3 backend haven't had the time to process the request above correctly this will generate an error.
	assert.NoError(s.T(), s.verificationDB.QueryRow("SELECT submission_file_size FROM sda.files WHERE id = $1;", fileID).Scan(&objectSize))
	assert.Equal(s.T(), int64(10*1024*1024), objectSize)
}
