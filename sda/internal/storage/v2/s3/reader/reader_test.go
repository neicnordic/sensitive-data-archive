package reader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storageerrors "github.com/neicnordic/sensitive-data-archive/internal/storage/v2/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/suite"
)

type ReaderTestSuite struct {
	suite.Suite
	reader *Reader

	configDir string

	s3Mock1, s3Mock2 *httptest.Server
}

func TestReaderTestSuite(t *testing.T) {
	suite.Run(t, new(ReaderTestSuite))
}

func (ts *ReaderTestSuite) SetupSuite() {

	ts.configDir = ts.T().TempDir()

	ts.s3Mock1 = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.RequestURI, "ListBuckets"):
			w.WriteHeader(http.StatusOK)

			_, _ = fmt.Fprint(w, `
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
   <Buckets>
      <Bucket>
         <BucketArn>mock_s3_1_bucket_1</BucketArn>
         <BucketRegion>us-east-1</BucketRegion>
         <Name>mock_s3_1_bucket_1</Name>
      </Bucket>
      <Bucket>
         <BucketArn>mock_s3_1_bucket_2</BucketArn>
         <BucketRegion>us-east-1</BucketRegion>
         <Name>mock_s3_1_bucket_2</Name>
      </Bucket>
   </Buckets>
   <Owner>
      <DisplayName>mock</DisplayName>
      <ID>mock</ID>
   </Owner>
</ListAllMyBucketsResult>
`)
		case strings.HasPrefix(req.RequestURI, "/mock_s3_1_bucket_1"):
			if req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject") && strings.Contains(req.RequestURI, "file1.txt") {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "file 1 content in mock s3 1, bucket 1")
				return
			}

			if req.Method == "HEAD" && strings.Contains(req.RequestURI, "file1.txt") {
				w.Header().Set("Content-Length", "101")
				w.WriteHeader(http.StatusOK)
				return
			}

			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, "no file found at: %s", req.RequestURI)

		case strings.HasPrefix(req.RequestURI, "/mock_s3_1_bucket_2"):
			if req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject") && strings.Contains(req.RequestURI, "file2.txt") {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "file 2 content in mock s3 1, bucket 2")
				return
			}

			if req.Method == "HEAD" && strings.Contains(req.RequestURI, "file2.txt") {
				w.Header().Set("Content-Length", "102")
				w.WriteHeader(http.StatusOK)
				return
			}

			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, "no file found at: %s", req.RequestURI)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "unexpected path called: %s", req.RequestURI)
		}
	}))

	ts.s3Mock2 = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Println(req.RequestURI)
		switch {
		case strings.HasSuffix(req.RequestURI, "ListBuckets"):
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
   <Buckets>
      <Bucket>
         <BucketArn>mock_s3_2_bucket_1</BucketArn>
         <BucketRegion>us-east-1</BucketRegion>
         <Name>mock_s3_2_bucket_1</Name>
      </Bucket>
      <Bucket>
         <BucketArn>mock_s3_2_bucket_2</BucketArn>
         <BucketRegion>us-east-1</BucketRegion>
         <Name>mock_s3_2_bucket_2</Name>
      </Bucket>
   </Buckets>
   <Owner>
      <DisplayName>mock</DisplayName>
      <ID>mock</ID>
   </Owner>
</ListAllMyBucketsResult>
`)
		case strings.HasPrefix(req.RequestURI, "/mock_s3_2_bucket_1"):
			if req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject") && strings.Contains(req.RequestURI, "file3.txt") {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "file 3 content in mock s3 2, bucket 1")
				return
			}

			if req.Method == "HEAD" && strings.Contains(req.RequestURI, "file3.txt") {
				w.Header().Set("Content-Length", "103")
				w.WriteHeader(http.StatusOK)
				return
			}

			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, "no file found at: %s", req.RequestURI)
		case strings.HasPrefix(req.RequestURI, "/mock_s3_2_bucket_2"):
			if req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject") && strings.Contains(req.RequestURI, "file4.txt") {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "file 4 content in mock s3 2, bucket 2")
				return
			}

			if req.Method == "HEAD" && strings.Contains(req.RequestURI, "file4.txt") {
				w.Header().Set("Content-Length", "104")
				w.WriteHeader(http.StatusOK)
				return
			}

			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, "no file found at: %s", req.RequestURI)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "unexpected path called: %s", req.RequestURI)
		}
	}))

	if err := os.WriteFile(filepath.Join(ts.configDir, "config.yaml"), []byte(fmt.Sprintf(`
storage:
  s3:
    test:
    - endpoint: %s
      access_key: access_key1
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
    - endpoint: %s
      access_key: access_key2
      secret_key: secret_key2
      disable_https: true
      region: us-east-1
`, ts.s3Mock1.URL, ts.s3Mock2.URL)), 0600); err != nil {
		ts.FailNow(err.Error())
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigType("yaml")
	viper.SetConfigFile(filepath.Join(ts.configDir, "config.yaml"))

	if err := viper.ReadInConfig(); err != nil {
		ts.FailNow(err.Error())
	}

	var err error
	ts.reader, err = NewReader(context.TODO(), "test")
	if err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom1Bucket1() {
	fileReader, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_1", "file1.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	content, err := io.ReadAll(fileReader)
	ts.NoError(err)

	ts.Equal("file 1 content in mock s3 1, bucket 1", string(content))
	_ = fileReader.Close()
}
func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom1Bucket2() {
	fileReader, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "file2.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	content, err := io.ReadAll(fileReader)
	ts.NoError(err)

	ts.Equal("file 2 content in mock s3 1, bucket 2", string(content))
	_ = fileReader.Close()
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom1Bucket1_NotFoundExpected() {
	_, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_1", "file2.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom1Bucket2_NotFoundExpected() {
	_, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom1Bucket1() {
	size, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_1", "file1.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(int64(101), size)
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom1Bucket2() {
	size, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "file2.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(int64(102), size)
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom1Bucket1_NotFoundExpected() {
	_, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_1", "file2.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom1Bucket2_NotFoundExpected() {
	_, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom2Bucket1() {
	fileReader, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "file3.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	content, err := io.ReadAll(fileReader)
	ts.NoError(err)

	ts.Equal("file 3 content in mock s3 2, bucket 1", string(content))
	_ = fileReader.Close()
}
func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom2Bucket2() {
	fileReader, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_2", "file4.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	content, err := io.ReadAll(fileReader)
	ts.NoError(err)

	ts.Equal("file 4 content in mock s3 2, bucket 2", string(content))
	_ = fileReader.Close()
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom2Bucket1_NotFoundExpected() {
	_, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_ReadFrom2Bucket2_NotFoundExpected() {
	_, err := ts.reader.NewFileReader(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_2", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom2Bucket1() {
	size, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "file3.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(int64(103), size)
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom2Bucket2() {
	size, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_2", "file4.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	ts.Equal(int64(104), size)
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom2Bucket1_NotFoundExpected() {
	_, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

func (ts *ReaderTestSuite) TestNewFileReader_GetFileSizeFrom2Bucket2_NotFoundExpected() {
	_, err := ts.reader.GetFileSize(context.Background(), ts.s3Mock2.URL+"/mock_s3_2_bucket_2", "file1.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}
