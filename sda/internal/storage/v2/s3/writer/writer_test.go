package writer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type WriterTestSuite struct {
	suite.Suite
	writer *Writer

	configDir string

	s3Mock1, s3Mock2   *mockS3
	locationBrokerMock *mockLocationBroker
}

type mockLocationBroker struct {
	mock.Mock
}
type mockS3 struct {
	server  *httptest.Server
	buckets map[string]map[string]string // "bucket name" -> "file name" -> "content"
}

func (m *mockS3) handler(w http.ResponseWriter, req *http.Request) {
	switch {
	case strings.HasSuffix(req.RequestURI, "PutObject"):
		m.PutObject(w, req)
	case strings.HasSuffix(req.RequestURI, "ListBuckets"):
		m.ListBuckets(w)
	case req.Method == "PUT":
		m.CreateBucket(w, req)
	case req.Method == "DELETE":
		m.Delete(w, req)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}
func (m *mockS3) ListBuckets(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)

	var b strings.Builder
	_, _ = b.WriteString(`
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
   <Buckets>`)

	for bucket := range m.buckets {
		_, _ = b.WriteString(fmt.Sprintf(`
      <Bucket>
         <BucketArn>%s</BucketArn>
         <BucketRegion>us-east-1</BucketRegion>
         <Name>%s</Name>
      </Bucket>`, bucket, bucket))
	}
	_, _ = b.WriteString(`
   </Buckets>
   <Owner>
      <DisplayName>mock</DisplayName>
      <ID>mock</ID>
   </Owner>
</ListAllMyBucketsResult>
`)
	_, _ = w.Write([]byte(b.String()))
}
func (m *mockS3) Delete(w http.ResponseWriter, req *http.Request) {
	bucket := strings.Split(req.RequestURI, "/")[1]
	fileName := strings.Split(strings.Split(req.RequestURI, "/")[2], "?")[0]

	delete(m.buckets[bucket], fileName)
	w.WriteHeader(http.StatusOK)
}
func (m *mockS3) PutObject(w http.ResponseWriter, req *http.Request) {
	bucket := strings.Split(req.RequestURI, "/")[1]

	if _, ok := m.buckets[bucket]; !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	content, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	fileName := strings.Split(strings.Split(req.RequestURI, "/")[2], "?")[0]

	m.buckets[bucket][fileName] = string(content)
}

func (m *mockS3) CreateBucket(w http.ResponseWriter, req *http.Request) {
	bucket := strings.TrimPrefix(req.RequestURI, "/")

	if _, ok := m.buckets[bucket]; ok {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	m.buckets[bucket] = make(map[string]string)
	w.WriteHeader(http.StatusOK)
}

func (m *mockLocationBroker) GetObjectCount(_ context.Context, location string) (uint64, error) {
	args := m.Called(location)
	count := args.Int(0)
	if count < 0 {
		count = 0
	}
	//nolint:gosec // disable G115
	return uint64(count), args.Error(1)
}

func (m *mockLocationBroker) GetSize(_ context.Context, location string) (uint64, error) {
	args := m.Called(location)
	size := args.Int(0)
	if size < 0 {
		size = 0
	}
	//nolint:gosec // disable G115
	return uint64(size), args.Error(1)
}

func TestReaderTestSuite(t *testing.T) {
	suite.Run(t, new(WriterTestSuite))
}

func (ts *WriterTestSuite) SetupSuite() {
	ts.configDir = ts.T().TempDir()

	ts.s3Mock1 = &mockS3{}
	ts.s3Mock1.buckets = map[string]map[string]string{}
	ts.s3Mock1.server = httptest.NewServer(http.HandlerFunc(ts.s3Mock1.handler))
	ts.s3Mock2 = &mockS3{}
	ts.s3Mock2.buckets = map[string]map[string]string{}
	ts.s3Mock2.server = httptest.NewServer(http.HandlerFunc(ts.s3Mock2.handler))

	if err := os.WriteFile(filepath.Join(ts.configDir, "config.yaml"), []byte(fmt.Sprintf(`
storage:
  test:
    s3:
    - endpoint: %s
      access_key: access_key1
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
      max_objects: 10
      max_size: 10kb
      max_buckets: 3
      bucket_prefix: bucket_in_1-
    - endpoint: %s
      access_key: access_key2
      secret_key: secret_key2
      disable_https: true
      region: us-east-1
      max_objects: 5
      max_size: 5kb
      max_buckets: 3
      bucket_prefix: bucket_in_2-
`, ts.s3Mock1.server.URL, ts.s3Mock2.server.URL)), 0600); err != nil {
		ts.FailNow(err.Error())
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetConfigFile(filepath.Join(ts.configDir, "config.yaml"))

	if err := viper.ReadInConfig(); err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *WriterTestSuite) SetupTest() {
	ts.s3Mock1.buckets = map[string]map[string]string{}
	ts.s3Mock2.buckets = map[string]map[string]string{}
	ts.locationBrokerMock = &mockLocationBroker{}

	var err error
	ts.writer, err = NewWriter(context.TODO(), "test", ts.locationBrokerMock)
	if err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *WriterTestSuite) TestWriteFile_AllEmpty() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL)).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL)).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL))
	ts.Equal(ts.s3Mock1.buckets["bucket_in_1-1"]["test_file_1.txt"], content)
	ts.Equal(fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL), location)
}
func (ts *WriterTestSuite) TestWriteFile_FirstFullSecondBucketExists() {
	content := "test file 1"

	ts.s3Mock1.buckets["bucket_in_1-2"] = make(map[string]string)
	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL)).Return(11, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL)).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL)).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL))
	ts.Equal(ts.s3Mock1.buckets["bucket_in_1-2"]["test_file_1.txt"], content)
	ts.Equal(fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL), location)
}

func (ts *WriterTestSuite) TestWriteFile_FirstEndpoint_FirstBucketFull() {
	content := "test file 1"

	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL)).Return(11, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(ts.s3Mock1.buckets["bucket_in_1-2"]["test_file_1.txt"], content)
	ts.Equal(fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL), location)
}

func (ts *WriterTestSuite) TestWriteFile_FirstEndpointFull() {
	content := "test file 2"

	ts.s3Mock1.buckets = map[string]map[string]string{"bucket_in_1-1": make(map[string]string), "bucket_in_1-2": make(map[string]string), "bucket_in_1-3": make(map[string]string)}
	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL)).Return(11, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL)).Return(11, nil).Once()
	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_1-3", ts.s3Mock1.server.URL)).Return(11, nil).Once()

	ts.locationBrokerMock.On("GetObjectCount", fmt.Sprintf("%s/bucket_in_2-1", ts.s3Mock2.server.URL)).Return(0, nil).Once()
	ts.locationBrokerMock.On("GetSize", fmt.Sprintf("%s/bucket_in_2-1", ts.s3Mock2.server.URL)).Return(0, nil).Once()

	contentReader := bytes.NewReader([]byte(content))
	location, err := ts.writer.WriteFile(context.TODO(), "test_file_1.txt", contentReader)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-2", ts.s3Mock1.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_1-3", ts.s3Mock1.server.URL))

	ts.locationBrokerMock.AssertCalled(ts.T(), "GetObjectCount", fmt.Sprintf("%s/bucket_in_2-1", ts.s3Mock2.server.URL))
	ts.locationBrokerMock.AssertCalled(ts.T(), "GetSize", fmt.Sprintf("%s/bucket_in_2-1", ts.s3Mock2.server.URL))
	ts.Equal(ts.s3Mock2.buckets["bucket_in_2-1"]["test_file_1.txt"], content)
	ts.Equal(fmt.Sprintf("%s/bucket_in_2-1", ts.s3Mock2.server.URL), location)
}

func (ts *WriterTestSuite) TestRemoveFile() {
	ts.s3Mock1.buckets["bucket_in_1-1"]["file_to_be_removed"] = "file to be removed content"

	err := ts.writer.RemoveFile(context.TODO(), fmt.Sprintf("%s/bucket_in_1-1", ts.s3Mock1.server.URL), "file_to_be_removed")
	ts.NoError(err)

	_, ok := ts.s3Mock1.buckets["bucket_in_1-1"]["file_to_be_removed"]
	ts.False(ok, "file to be removed still exists")
}

func (ts *WriterTestSuite) TestRemoveFile_InvalidLocation() {
	err := ts.writer.RemoveFile(context.TODO(), "http://different_s3_url/bucket", "file_to_be_removed")
	ts.EqualError(err, storageerrors.ErrorNoEndpointConfiguredForLocation.Error())
}
