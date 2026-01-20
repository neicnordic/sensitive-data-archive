package reader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/neicnordic/sensitive-data-archive/internal/storage/v2/storageerrors"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ReaderTestSuite struct {
	suite.Suite
	reader *Reader

	configDir string

	bigFilePath      string
	s3Mock1, s3Mock2 *httptest.Server
}

func TestReaderTestSuite(t *testing.T) {
	suite.Run(t, new(ReaderTestSuite))
}

func (ts *ReaderTestSuite) SetupSuite() {
	ts.configDir = ts.T().TempDir()

	// create a big file
	bigFile, err := os.CreateTemp(ts.configDir, "bigfile-")
	if err != nil {
		ts.FailNow("failed to create big test file", err)
	}

	if _, err = bigFile.WriteString("This is a big file for testing seekable s3 reader"); err != nil {
		ts.FailNow("failed to write big test file", err)
	}
	for range 6 * 1000 * 1000 {
		if _, err = bigFile.WriteString("a"); err != nil {
			ts.FailNow("failed to write big test file", err)
		}
	}
	if _, err = bigFile.WriteString("file is ending now"); err != nil {
		ts.FailNow("failed to write big test file", err)
	}
	_ = bigFile.Close()
	ts.bigFilePath = bigFile.Name()

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

			if strings.Contains(req.RequestURI, "seekable_big_file.txt") {
				content, err := os.ReadFile(ts.bigFilePath)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprintf(w, "could not read the big seekable file due to: %v", err)
				}

				switch {
				case req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject"):
					byteRange := req.Header.Get("Range")
					start, _ := strconv.Atoi(strings.Split(strings.Split(byteRange, "=")[1], "-")[0])
					end, _ := strconv.Atoi(strings.Split(strings.Split(byteRange, "=")[1], "-")[1])
					if len(content) < end {
						end = len(content)
					}
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content), len(content)))
					seekedContent := content[start:end]
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, string(seekedContent))

				case req.Method == "HEAD":
					w.Header().Set("Content-Length", strconv.Itoa(len(content)))
					w.WriteHeader(http.StatusOK)
				default:
				}

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

			if req.Method == "GET" && strings.HasSuffix(req.RequestURI, "GetObject") && strings.Contains(req.RequestURI, "seekable.txt") {
				byteRange := req.Header.Get("Range")
				start, _ := strconv.Atoi(strings.Split(strings.Split(byteRange, "=")[1], "-")[0])
				end, _ := strconv.Atoi(strings.Split(strings.Split(byteRange, "=")[1], "-")[1])
				content := "this file is mocked to be seekable following s3 logic in mock s3 2, bucket 1"

				if len(content) < end {
					end = len(content)
				}
				seekedContent := content[start:end]
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
				w.WriteHeader(http.StatusOK)

				_, _ = fmt.Fprint(w, seekedContent)

				return
			}

			if req.Method == "HEAD" && strings.Contains(req.RequestURI, "seekable.txt") {
				w.Header().Set("Content-Length", "77")
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
  test:
    s3:
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

	ts.reader, err = NewReader(context.TODO(), "test")
	if err != nil {
		ts.FailNow(err.Error())
	}
}

func (ts *ReaderTestSuite) TestLoadConfig() {
	for _, test := range []struct {
		testName         string
		config           string
		expectedErrorMsg string
	}{
		{
			testName: "MissingEndpoint",
			config: `
storage:
  config_test:
    s3:
    - access_key: access_key1
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
      chunk_size: 1mb
`,
			expectedErrorMsg: "missing required parameter: endpoint",
		}, {
			testName: "MissingAccessKey",
			config: `
storage:
  config_test:
    s3:
    - endpoint: 123
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
      chunk_size: 1mb
`,
			expectedErrorMsg: "missing required parameter: access_key",
		}, {
			testName: "MissingSecretKey",
			config: `
storage:
  config_test:
    s3:
    - endpoint: 123
      access_key: access_key1
      disable_https: true
      region: us-east-1
      chunk_size: 1mb
`,
			expectedErrorMsg: "missing required parameter: secret_key",
		}, {
			testName: "InvalidChunkSize",
			config: `
storage:
  config_test:
    s3:
    - endpoint: 123
      access_key: access_key1
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
      chunk_size: -100
`,
			expectedErrorMsg: "could not parse chunk_size as a valid data size",
		}, {
			testName: "HTTPSEndpointWithDisableHttps",
			config: `
storage:
  config_test:
    s3:
    - endpoint: https://123
      access_key: access_key1
      secret_key: secret_key1
      disable_https: true
      region: us-east-1
      chunk_size: -100
`,
			expectedErrorMsg: "https scheme in endpoint when HTTPS is disabled",
		}, {
			testName: "HTTPEndpointWithEnabledHttps",
			config: `
storage:
  config_test:
    s3:
    - endpoint: http://123
      access_key: access_key1
      secret_key: secret_key1
      disable_https: false
      region: us-east-1
      chunk_size: -100
`,
			expectedErrorMsg: "http scheme in endpoint when using HTTPS",
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(ts.configDir, "config_.yaml"), []byte(test.config), 0600); err != nil {
				assert.FailNow(t, err.Error())
			}

			viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			viper.SetConfigType("yaml")
			viper.SetConfigFile(filepath.Join(ts.configDir, "config_.yaml"))

			if err := viper.ReadInConfig(); err != nil {
				assert.FailNow(t, err.Error())
			}

			_, err := NewReader(context.TODO(), "config_test")
			assert.EqualError(t, err, test.expectedErrorMsg)
		})
	}
}

func (ts *ReaderTestSuite) TestNewFileReader() {
	for _, test := range []struct {
		testName string

		endpointToReadFrom  string
		bucketToReadFrom    string
		filePathToRead      string
		expectedError       error
		expectedFileContent string
	}{
		{
			testName:            "InvalidLocation",
			endpointToReadFrom:  "",
			bucketToReadFrom:    "",
			filePathToRead:      "",
			expectedError:       storageerrors.ErrorInvalidLocation,
			expectedFileContent: "",
		}, {
			testName:            "ReadFrom1Bucket1",
			endpointToReadFrom:  ts.s3Mock1.URL,
			bucketToReadFrom:    "mock_s3_1_bucket_1",
			filePathToRead:      "file1.txt",
			expectedError:       nil,
			expectedFileContent: "file 1 content in mock s3 1, bucket 1",
		}, {
			testName:            "ReadFrom1Bucket2",
			endpointToReadFrom:  ts.s3Mock1.URL,
			bucketToReadFrom:    "mock_s3_1_bucket_2",
			filePathToRead:      "file2.txt",
			expectedError:       nil,
			expectedFileContent: "file 2 content in mock s3 1, bucket 2",
		}, {
			testName:            "ReadFrom2Bucket1",
			endpointToReadFrom:  ts.s3Mock2.URL,
			bucketToReadFrom:    "mock_s3_2_bucket_1",
			filePathToRead:      "file3.txt",
			expectedError:       nil,
			expectedFileContent: "file 3 content in mock s3 2, bucket 1",
		}, {
			testName:            "ReadFrom2Bucket2",
			endpointToReadFrom:  ts.s3Mock2.URL,
			bucketToReadFrom:    "mock_s3_2_bucket_2",
			filePathToRead:      "file4.txt",
			expectedError:       nil,
			expectedFileContent: "file 4 content in mock s3 2, bucket 2",
		}, {
			testName:            "ReadFrom1Bucket1_NotFound",
			endpointToReadFrom:  ts.s3Mock1.URL,
			bucketToReadFrom:    "mock_s3_1_bucket_1",
			filePathToRead:      "file2.txt",
			expectedError:       storageerrors.ErrorFileNotFoundInLocation,
			expectedFileContent: "",
		}, {
			testName:            "ReadFrom1Bucket2_NotFound",
			endpointToReadFrom:  ts.s3Mock1.URL,
			bucketToReadFrom:    "mock_s3_1_bucket_2",
			filePathToRead:      "file1.txt",
			expectedError:       storageerrors.ErrorFileNotFoundInLocation,
			expectedFileContent: "",
		}, {
			testName:            "ReadFrom2Bucket1_NotFound",
			endpointToReadFrom:  ts.s3Mock2.URL,
			bucketToReadFrom:    "mock_s3_2_bucket_1",
			filePathToRead:      "file1.txt",
			expectedError:       storageerrors.ErrorFileNotFoundInLocation,
			expectedFileContent: "",
		}, {
			testName:            "ReadFrom2Bucket2_NotFound",
			endpointToReadFrom:  ts.s3Mock2.URL,
			bucketToReadFrom:    "mock_s3_2_bucket_2",
			filePathToRead:      "file1.txt",
			expectedError:       storageerrors.ErrorFileNotFoundInLocation,
			expectedFileContent: "",
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			fileReader, err := ts.reader.NewFileReader(context.TODO(), test.endpointToReadFrom+"/"+test.bucketToReadFrom, test.filePathToRead)
			assert.Equal(t, test.expectedError, err)

			if fileReader != nil {
				content, err := io.ReadAll(fileReader)
				assert.NoError(t, err)

				assert.Equal(t, test.expectedFileContent, string(content))
				_ = fileReader.Close()
			}
		})
	}
}

func (ts *ReaderTestSuite) TestGetFileSize() {
	for _, test := range []struct {
		testName string

		endpointToReadFrom string
		bucketToReadFrom   string
		filePathToRead     string
		expectedError      error
		expectedSize       int64
	}{
		{
			testName:           "InvalidLocation",
			endpointToReadFrom: "",
			bucketToReadFrom:   "",
			filePathToRead:     "",
			expectedError:      storageerrors.ErrorInvalidLocation,
			expectedSize:       0,
		}, {
			testName:           "ReadFrom1Bucket1",
			endpointToReadFrom: ts.s3Mock1.URL,
			bucketToReadFrom:   "mock_s3_1_bucket_1",
			filePathToRead:     "file1.txt",
			expectedError:      nil,
			expectedSize:       101,
		}, {
			testName:           "ReadFrom1Bucket2",
			endpointToReadFrom: ts.s3Mock1.URL,
			bucketToReadFrom:   "mock_s3_1_bucket_2",
			filePathToRead:     "file2.txt",
			expectedError:      nil,
			expectedSize:       102,
		}, {
			testName:           "ReadFrom2Bucket1",
			endpointToReadFrom: ts.s3Mock2.URL,
			bucketToReadFrom:   "mock_s3_2_bucket_1",
			filePathToRead:     "file3.txt",
			expectedError:      nil,
			expectedSize:       103,
		}, {
			testName:           "ReadFrom2Bucket2",
			endpointToReadFrom: ts.s3Mock2.URL,
			bucketToReadFrom:   "mock_s3_2_bucket_2",
			filePathToRead:     "file4.txt",
			expectedError:      nil,
			expectedSize:       104,
		}, {
			testName:           "ReadFrom1Bucket1_NotFound",
			endpointToReadFrom: ts.s3Mock1.URL,
			bucketToReadFrom:   "mock_s3_1_bucket_1",
			filePathToRead:     "file2.txt",
			expectedError:      storageerrors.ErrorFileNotFoundInLocation,
			expectedSize:       0,
		}, {
			testName:           "ReadFrom1Bucket2_NotFound",
			endpointToReadFrom: ts.s3Mock1.URL,
			bucketToReadFrom:   "mock_s3_1_bucket_2",
			filePathToRead:     "file1.txt",
			expectedError:      storageerrors.ErrorFileNotFoundInLocation,
			expectedSize:       0,
		}, {
			testName:           "ReadFrom2Bucket2_NotFound",
			endpointToReadFrom: ts.s3Mock2.URL,
			bucketToReadFrom:   "mock_s3_2_bucket_2",
			filePathToRead:     "file1.txt",
			expectedError:      storageerrors.ErrorFileNotFoundInLocation,
			expectedSize:       0,
		}, {
			testName:           "ReadFrom2Bucket2_NotFound",
			endpointToReadFrom: ts.s3Mock2.URL,
			bucketToReadFrom:   "mock_s3_2_bucket_2",
			filePathToRead:     "file1.txt",
			expectedError:      storageerrors.ErrorFileNotFoundInLocation,
			expectedSize:       0,
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			size, err := ts.reader.GetFileSize(context.TODO(), test.endpointToReadFrom+"/"+test.bucketToReadFrom, test.filePathToRead)
			assert.Equal(t, test.expectedError, err)
			assert.Equal(t, test.expectedSize, size)
		})
	}
}

func (ts *ReaderTestSuite) TestFindFile() {
	for _, test := range []struct {
		testName string

		fileToFind       string
		expectedLocation string
		expectedError    error
	}{
		{
			testName:         "NotFound",
			fileToFind:       "not_found.txt",
			expectedLocation: "",
			expectedError:    storageerrors.ErrorFileNotFoundInLocation,
		}, {
			testName:         "FoundIn1Bucket1",
			fileToFind:       "file1.txt",
			expectedLocation: ts.s3Mock1.URL + "/mock_s3_1_bucket_1",
			expectedError:    nil,
		}, {
			testName:         "FoundIn1Bucket2",
			fileToFind:       "file2.txt",
			expectedLocation: ts.s3Mock1.URL + "/mock_s3_1_bucket_2",
			expectedError:    nil,
		}, {
			testName:         "FoundIn2Bucket1",
			fileToFind:       "file3.txt",
			expectedLocation: ts.s3Mock2.URL + "/mock_s3_2_bucket_1",
			expectedError:    nil,
		}, {
			testName:         "FoundIn2Bucket2",
			fileToFind:       "file4.txt",
			expectedLocation: ts.s3Mock2.URL + "/mock_s3_2_bucket_2",
			expectedError:    nil,
		},
	} {
		ts.T().Run(test.testName, func(t *testing.T) {
			location, err := ts.reader.FindFile(context.TODO(), test.fileToFind)
			assert.Equal(t, test.expectedError, err)
			assert.Equal(t, test.expectedLocation, location)
		})
	}
}

func (ts *ReaderTestSuite) TestNewFileSeekReader_ReadFrom2Bucket1() {
	fileSeekReader, err := ts.reader.NewFileReadSeeker(context.TODO(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "seekable.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	start := int64(5)
	lenToRead := int64(14)

	_, err = fileSeekReader.Seek(start, 0)
	ts.NoError(err)

	content := make([]byte, lenToRead)

	read, err := fileSeekReader.Read(content)
	ts.NoError(err)
	ts.Equal(lenToRead, int64(read))

	ts.Equal("file is mocked", string(content))
	_ = fileSeekReader.Close()
}

func (ts *ReaderTestSuite) TestNewFileSeekReader_ReadFrom1Bucket2_BigFile() {
	fileSeekReader, err := ts.reader.NewFileReadSeeker(context.TODO(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "seekable_big_file.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}

	start := int64(14)
	end := int64(6000060)

	_, err = fileSeekReader.Seek(start, 0)
	ts.NoError(err)

	togo := end - start

	buf := make([]byte, 4096)

	var content []byte
	// Loop until we've read what we should (if no/faulty end given, that's EOF)
	for end == 0 || togo > 0 {
		rbuf := buf

		if end != 0 && togo < 4096 {
			// If we don't want to read as much as 4096 bytes
			rbuf = buf[:togo]
		}
		r, err := fileSeekReader.Read(rbuf)
		togo -= int64(r)

		if err == io.EOF && r == 0 {
			break
		}

		if err != nil && err != io.EOF {
			ts.FailNow(err.Error())
		}

		content = append(content, rbuf[:r]...)
	}

	ts.NoError(err)
	ts.Equal(end-start, int64(len(content)))

	ts.Equal("aaaaaaafile is end", string(content[len(content)-len("aaaaaaafile is end"):]))
	ts.Equal("file for testing seekable s3 readeraaaaaaaaaa", string(content[:len("file for testing seekable s3 readeraaaaaaaaaa")]))
	_ = fileSeekReader.Close()
}

func (ts *ReaderTestSuite) TestNewFileSeekReader_ReadFrom2Bucket1FileNotFound() {
	_, err := ts.reader.NewFileReadSeeker(context.TODO(), ts.s3Mock2.URL+"/mock_s3_2_bucket_1", "not_exist.txt")
	ts.EqualError(err, storageerrors.ErrorFileNotFoundInLocation.Error())
}

// s3 seekable reader internal test here, messing with internals to expose behaviour
func (ts *ReaderTestSuite) TestFileReadSeeker_Internal_NoPrefetchFromSeek() {
	fileSeekReader, err := ts.reader.NewFileReadSeeker(context.TODO(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "seekable_big_file.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}
	s3SeekableReaderCast, ok := fileSeekReader.(*s3SeekableReader)
	if !ok {
		ts.FailNow("could not cast io.ReadSeekerCloser to *s3SeekableReader returned from NewFileReadSeeker")
	}
	s3SeekableReaderCast.seeked = true

	var readBackBuffer [4096]byte

	_, err = s3SeekableReaderCast.Read(readBackBuffer[0:4096])
	ts.Equal("This is a big file", string(readBackBuffer[:18]), "did not read back data as expected")
	ts.NoError(err, "read returned unexpected error")
	_ = fileSeekReader.Close()
}

// s3 seekable reader internal test here, messing with internals to expose behaviour
func (ts *ReaderTestSuite) TestFileReadSeeker_Internal_Cache() {
	fileSeekReader, err := ts.reader.NewFileReadSeeker(context.TODO(), ts.s3Mock1.URL+"/mock_s3_1_bucket_2", "seekable_big_file.txt")
	if err != nil {
		ts.FailNow(err.Error())
	}
	s3SeekableReaderCast, ok := fileSeekReader.(*s3SeekableReader)
	if !ok {
		ts.FailNow("could not cast io.ReadSeekerCloser to *s3SeekableReader returned from NewFileReadSeeker")
	}
	s3SeekableReaderCast.seeked = true

	s3SeekableReaderCast.prefetchAt(0)
	ts.Equal(1, len(s3SeekableReaderCast.local), "nothing cached after prefetch")
	// Clear cache
	s3SeekableReaderCast.local = s3SeekableReaderCast.local[:0]

	s3SeekableReaderCast.outstandingPrefetches = []int64{0}

	var readBackBuffer [4096]byte
	n, err := s3SeekableReaderCast.Read(readBackBuffer[0:4096])
	ts.Nil(err, "read returned unexpected error")
	ts.Equal(0, n, "got data when we should get 0 because of prefetch")

	for i := 0; i < 30; i++ {
		s3SeekableReaderCast.local = append(s3SeekableReaderCast.local, s3CacheBlock{90000000, int64(0), nil})
	}
	s3SeekableReaderCast.prefetchAt(0)
	ts.Equal(8, len(s3SeekableReaderCast.local), "unexpected length of cache after prefetch")

	s3SeekableReaderCast.outstandingPrefetches = []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s3SeekableReaderCast.removeFromOutstanding(9)
	ts.Equal(s3SeekableReaderCast.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s3SeekableReaderCast.removeFromOutstanding(19)
	ts.Equal(s3SeekableReaderCast.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s3SeekableReaderCast.removeFromOutstanding(5)
	// We don't care about the internal order, sort for simplicity
	slices.Sort(s3SeekableReaderCast.outstandingPrefetches)
	ts.Equal(s3SeekableReaderCast.outstandingPrefetches, []int64{0, 1, 2, 3, 4, 6, 7, 8}, "unexpected outstanding prefetches after remove")

	s3SeekableReaderCast.objectReader = nil
	s3SeekableReaderCast.bucket = ""
	s3SeekableReaderCast.filePath = ""
	data := make([]byte, 100)
	_, err = s3SeekableReaderCast.wholeReader(data)
	ts.NotNil(err, "wholeReader object instantiation worked when it should have failed")
}

func (ts *ReaderTestSuite) TestNewFileReaderSeeker_InvalidLocation() {
	_, err := ts.reader.NewFileReader(context.TODO(), "", "")
	ts.EqualError(err, storageerrors.ErrorInvalidLocation.Error())
}
