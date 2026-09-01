package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lestrrat-go/jwx/v2/jwt"
	broker "github.com/neicnordic/sensitive-data-archive/internal/broker/v2"
	"github.com/neicnordic/sensitive-data-archive/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestProxyAllowedS3Actions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		req      *http.Request
		newMocks func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer)
	}{
		{
			name: "CompleteMultipartUpload",
			req:  httptest.NewRequest(http.MethodPost, "/unit_test_user/test?uploadId=upload-id", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("file_id_123", nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(404, nil, nil).Once()
				ms.On("ServeHTTP", http.MethodPost, "/test_inbox_bucket/unit_test_user/test", "uploadId=upload-id").Return(200, nil, nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, map[string]string{"ETag": "shaa", "Content-Length": "321"}).Twice()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"operation":"upload","user":"unit_test_user","filepath":"unit_test_user/test","filesize":321,"encrypted_checksums":[{"type":"md5","value":"shaa"}]}`),
				}).Return(nil).Once()

				mdb.On("SetSubmissionFileSize", "file_id_123", int64(321)).Return(nil).Once()
				mdb.On("UpdateFileEventLog", "file_id_123", "uploaded", "inbox", "{}", mock.Anything).Return(nil).Once()

				return mdb, mb, ms
			},
		},
		{
			name: "CompleteMultipartUpload_Reupload",
			req:  httptest.NewRequest(http.MethodPost, "/unit_test_user/test?uploadId=upload-id", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("file_id_123", nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, map[string]string{"ETag": "shaa", "Content-Length": "321"}).Times(3)
				ms.On("ServeHTTP", http.MethodPost, "/test_inbox_bucket/unit_test_user/test", "uploadId=upload-id").Return(200, nil, nil).Once()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"user":"unit_test_user","filepath":"unit_test_user/test","operation":"remove"}`),
				}).Return(nil).Once()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"operation":"upload","user":"unit_test_user","filepath":"unit_test_user/test","filesize":321,"encrypted_checksums":[{"type":"md5","value":"shaa"}]}`),
				}).Return(nil).Once()

				mdb.On("SetSubmissionFileSize", "file_id_123", int64(321)).Return(nil).Once()
				mdb.On("UpdateFileEventLog", "file_id_123", "uploaded", "inbox", "{}", mock.Anything).Return(nil).Once()

				return mdb, mb, ms
			},
		},
		{
			name: "CreateMultipartUpload",
			req:  httptest.NewRequest(http.MethodPost, "/unit_test_user/test?uploads", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("", nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()

				mdb.On("RegisterFile", (*string)(nil), mock.MatchedBy(func(loc string) bool {
					// cant verify the port as the mock s3 server gets different port each run by httptest, so just verify it sets the bucket and format correctly
					u, err := url.Parse(loc)

					return err == nil &&
						u.Scheme == "http" &&
						u.Hostname() == "127.0.0.1" &&
						u.Path == "/test_inbox_bucket"
				}), "test", "unit_test_user").Return("file_id_123", nil).Once()
				mdb.On("Commit").Return(nil).Once()
				ms.On("ServeHTTP", http.MethodPost, "/test_inbox_bucket/unit_test_user/test", "uploads=").Return(200, nil, nil).Once()

				return mdb, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "CreateMultipartUpload_AlreadyExists",
			req:  httptest.NewRequest(http.MethodPost, "/unit_test_user/test?uploads", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("file_id_123", nil).Once()
				ms.On("ServeHTTP", http.MethodPost, "/test_inbox_bucket/unit_test_user/test", "uploads=").Return(200, nil, nil).Once()

				return mdb, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "PutObject",
			req:  httptest.NewRequest(http.MethodPut, "/unit_test_user/test", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("", nil).Once()
				mdb.On("BeginTransaction").Return(nil).Once()
				mdb.On("RegisterFile", (*string)(nil), mock.MatchedBy(func(loc string) bool {
					// cant verify the port as the mock s3 server gets different port each run by httptest, so just verify it sets the bucket and format correctly
					u, err := url.Parse(loc)

					return err == nil &&
						u.Scheme == "http" &&
						u.Hostname() == "127.0.0.1" &&
						u.Path == "/test_inbox_bucket"
				}), "test", "unit_test_user").Return("file_id_123", nil).Once()
				mdb.On("Commit").Return(nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(404, nil, nil).Once()
				ms.On("ServeHTTP", http.MethodPut, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, map[string]string{"ETag": "shaa", "Content-Length": "321"}).Twice()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"operation":"upload","user":"unit_test_user","filepath":"unit_test_user/test","filesize":321,"encrypted_checksums":[{"type":"md5","value":"shaa"}]}`),
				}).Return(nil).Once()

				mdb.On("SetSubmissionFileSize", "file_id_123", int64(321)).Return(nil).Once()
				mdb.On("UpdateFileEventLog", "file_id_123", "uploaded", "inbox", "{}", mock.Anything).Return(nil).Once()

				return mdb, mb, ms
			},
		}, {
			name: "PutObject_AlreadyExists",
			req:  httptest.NewRequest(http.MethodPut, "/unit_test_user/test", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				mdb := &mocks.MockDatabase{}
				mb := &mocks.MockBroker{}
				ms := &mockServer{}

				mdb.On("GetFileIDInInbox", "unit_test_user", "test").Return("file_id_123", nil).Once()

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, map[string]string{"ETag": "shaa", "Content-Length": "321"}).Times(3)
				ms.On("ServeHTTP", http.MethodPut, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, nil).Once()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"user":"unit_test_user","filepath":"unit_test_user/test","operation":"remove"}`),
				}).Return(nil).Once()

				mb.On("Publish", "unit-test_destination", broker.Message{
					Key:  "file_id_123",
					Body: []byte(`{"operation":"upload","user":"unit_test_user","filepath":"unit_test_user/test","filesize":321,"encrypted_checksums":[{"type":"md5","value":"shaa"}]}`),
				}).Return(nil).Once()

				mdb.On("SetSubmissionFileSize", "file_id_123", int64(321)).Return(nil).Once()
				mdb.On("UpdateFileEventLog", "file_id_123", "uploaded", "inbox", "{}", mock.Anything).Return(nil).Once()

				return mdb, mb, ms
			},
		},
		{
			name: "AbortMultipartUpload",
			req:  httptest.NewRequest(http.MethodDelete, "/unit_test_user/test?uploadId=upload-id", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodDelete, "/test_inbox_bucket/unit_test_user/test", "uploadId=upload-id").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "GetBucketLocation",
			req:  httptest.NewRequest(http.MethodGet, "/unit_test_user?location", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodGet, "/test_inbox_bucket", "location=").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "ListObjects",
			req:  httptest.NewRequest(http.MethodGet, "/unit_test_user", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodGet, "/test_inbox_bucket", "prefix=unit_test_user%2F").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "ListObjectsV2",
			req:  httptest.NewRequest(http.MethodGet, "/unit_test_user?list-type=2", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodGet, "/test_inbox_bucket", "list-type=2&prefix=unit_test_user%2F").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "UploadPart",
			req:  httptest.NewRequest(http.MethodPut, "/unit_test_user/test?partNumber=1&uploadId=upload-id", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodPut, "/test_inbox_bucket/unit_test_user/test", "partNumber=1&uploadId=upload-id").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "ListParts",
			req:  httptest.NewRequest(http.MethodGet, "/unit_test_user/test?uploadId=upload-id", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodGet, "/test_inbox_bucket/unit_test_user/test", "uploadId=upload-id").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "ListMultipartUploads",
			req:  httptest.NewRequest(http.MethodGet, "/unit_test_user?uploads", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodGet, "/test_inbox_bucket", "prefix=unit_test_user%2F&uploads=").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
		{
			name: "HeadObject",
			req:  httptest.NewRequest(http.MethodHead, "/unit_test_user/test", nil),
			newMocks: func() (*mocks.MockDatabase, *mocks.MockBroker, *mockServer) {
				ms := &mockServer{}

				ms.On("ServeHTTP", http.MethodHead, "/test_inbox_bucket/unit_test_user/test", "").Return(200, nil, nil).Once()

				return &mocks.MockDatabase{}, &mocks.MockBroker{}, ms
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDatabase, mockBroker, mockS3ServerImpl := tc.newMocks()

			s3MockServer := httptest.NewServer(http.HandlerFunc(mockS3ServerImpl.ServeHTTP))
			defer s3MockServer.Close()
			s3Client := s3.New(s3.Options{
				BaseEndpoint: aws.String(s3MockServer.URL),
				Region:       "test",
				Credentials:  credentials.NewStaticCredentialsProvider("unit_test_access_key", "unit_test_secret_key", ""),
			})

			ma := &mockAuthenticator{}
			testToken := jwt.New()
			_ = testToken.Set("sub", "unit_test_user")
			ma.On("Authenticate", mock.Anything).Once().Return(testToken, nil)

			p := &proxy{
				s3Conf: s3InboxConfig{
					endpoint:  s3MockServer.URL,
					accessKey: "unit_test_access_key",
					secretKey: "unit_test_secret_key",
					bucket:    "test_inbox_bucket",
				},
				s3Client:         s3Client,
				auth:             ma,
				broker:           mockBroker,
				database:         mockDatabase,
				client:           &http.Client{},
				destinationQueue: "unit-test_destination",
			}

			w := httptest.NewRecorder()
			p.ServeHTTP(w, tc.req)

			assert.Equal(t, 200, w.Code)
			ma.AssertExpectations(t)
			mockDatabase.AssertExpectations(t)
			mockBroker.AssertExpectations(t)
			mockS3ServerImpl.AssertExpectations(t)
		})
	}
}
func TestProxyForbiddenS3Actions(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{
			name: "CopyObject",
			req: func() *http.Request {
				req := httptest.NewRequest(http.MethodPut, "/test/test", nil)
				req.Header.Set("x-amz-copy-source", "/from/source")

				return req
			}(),
		},
		{
			name: "CreateBucket",
			req:  httptest.NewRequest(http.MethodPut, "/test", nil),
		},
		{
			name: "CreateBucketMetadataConfiguration",
			req:  httptest.NewRequest(http.MethodPost, "/test?metadata", nil),
		},
		{
			name: "CreateBucketMetadataTableConfiguration",
			req:  httptest.NewRequest(http.MethodPost, "/test?metadataTable", nil),
		},
		{
			name: "CreateSession",
			req:  httptest.NewRequest(http.MethodPost, "/test?session", nil),
		},
		{
			name: "DeleteBucket",
			req:  httptest.NewRequest(http.MethodDelete, "/test", nil),
		},
		{
			name: "DeleteBucketAnalyticsConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?analytics&id=id", nil),
		},
		{
			name: "DeleteBucketCors",
			req:  httptest.NewRequest(http.MethodDelete, "/test?cors", nil),
		},
		{
			name: "DeleteBucketEncryption",
			req:  httptest.NewRequest(http.MethodDelete, "/test?encryption", nil),
		},
		{
			name: "DeleteBucketIntelligentTieringConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?intelligent-tiering&id=id", nil),
		},
		{
			name: "DeleteBucketInventoryConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?inventory&id=id", nil),
		},
		{
			name: "DeleteBucketLifecycle",
			req:  httptest.NewRequest(http.MethodDelete, "/test?lifecycle", nil),
		},
		{
			name: "DeleteBucketMetadataConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?metadata", nil),
		},
		{
			name: "DeleteBucketMetadataTableConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?metadataTable", nil),
		},
		{
			name: "DeleteBucketMetricsConfiguration",
			req:  httptest.NewRequest(http.MethodDelete, "/test?metrics&id=id", nil),
		},
		{
			name: "DeleteBucketOwnershipControls",
			req:  httptest.NewRequest(http.MethodDelete, "/test?ownershipControls", nil),
		},
		{
			name: "DeleteBucketPolicy",
			req:  httptest.NewRequest(http.MethodDelete, "/test?policy", nil),
		},
		{
			name: "DeleteBucketReplication",
			req:  httptest.NewRequest(http.MethodDelete, "/test?replication", nil),
		},
		{
			name: "DeleteBucketTagging",
			req:  httptest.NewRequest(http.MethodDelete, "/test?tagging", nil),
		},
		{
			name: "DeleteBucketWebsite",
			req:  httptest.NewRequest(http.MethodDelete, "/test?website", nil),
		},
		{
			name: "DeleteObject",
			req:  httptest.NewRequest(http.MethodDelete, "/test/test", nil),
		},
		{
			name: "DeleteObjectAnnotation",
			req:  httptest.NewRequest(http.MethodDelete, "/test/test?annotation", nil),
		},
		{
			name: "DeleteObjects",
			req:  httptest.NewRequest(http.MethodPost, "/test?delete", nil),
		},
		{
			name: "DeleteObjectTagging",
			req:  httptest.NewRequest(http.MethodDelete, "/test/test?tagging", nil),
		},
		{
			name: "DeletePublicAccessBlock",
			req:  httptest.NewRequest(http.MethodDelete, "/test?publicAccessBlock", nil),
		},
		{
			name: "GetBucketAbac",
			req:  httptest.NewRequest(http.MethodGet, "/test?abac", nil),
		},
		{
			name: "GetBucketAccelerateConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?accelerate", nil),
		},
		{
			name: "GetBucketAcl",
			req:  httptest.NewRequest(http.MethodGet, "/test?acl", nil),
		},
		{
			name: "GetBucketAnalyticsConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?analytics&id=id", nil),
		},
		{
			name: "GetBucketCors",
			req:  httptest.NewRequest(http.MethodGet, "/test?cors", nil),
		},
		{
			name: "GetBucketEncryption",
			req:  httptest.NewRequest(http.MethodGet, "/test?encryption", nil),
		},
		{
			name: "GetBucketIntelligentTieringConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?intelligent-tiering&id=id", nil),
		},
		{
			name: "GetBucketInventoryConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?inventory&id=id", nil),
		},
		{
			name: "GetBucketLifecycle",
			req:  httptest.NewRequest(http.MethodGet, "/test?lifecycle", nil),
		},
		{
			name: "GetBucketLifecycleConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?lifecycle", nil),
		},
		{
			name: "GetBucketLogging",
			req:  httptest.NewRequest(http.MethodGet, "/test?logging", nil),
		},
		{
			name: "GetBucketMetadataConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?metadata", nil),
		},
		{
			name: "GetBucketMetadataTableConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?metadataTable", nil),
		},
		{
			name: "GetBucketMetricsConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?metrics&id=id", nil),
		},
		{
			name: "GetBucketNotification",
			req:  httptest.NewRequest(http.MethodGet, "/test?notification", nil),
		},
		{
			name: "GetBucketNotificationConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?notification", nil),
		},
		{
			name: "GetBucketOwnershipControls",
			req:  httptest.NewRequest(http.MethodGet, "/test?ownershipControls", nil),
		},
		{
			name: "GetBucketPolicy",
			req:  httptest.NewRequest(http.MethodGet, "/test?policy", nil),
		},
		{
			name: "GetBucketPolicyStatus",
			req:  httptest.NewRequest(http.MethodGet, "/test?policyStatus", nil),
		},
		{
			name: "GetBucketReplication",
			req:  httptest.NewRequest(http.MethodGet, "/test?replication", nil),
		},
		{
			name: "GetBucketRequestPayment",
			req:  httptest.NewRequest(http.MethodGet, "/test?requestPayment", nil),
		},
		{
			name: "GetBucketTagging",
			req:  httptest.NewRequest(http.MethodGet, "/test?tagging", nil),
		},
		{
			name: "GetBucketVersioning",
			req:  httptest.NewRequest(http.MethodGet, "/test?versioning", nil),
		},
		{
			name: "GetBucketWebsite",
			req:  httptest.NewRequest(http.MethodGet, "/test?website", nil),
		},
		{
			name: "GetObject",
			req:  httptest.NewRequest(http.MethodGet, "/test/test", nil),
		},
		{
			name: "GetObjectAcl",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?acl", nil),
		},
		{
			name: "GetObjectAnnotation",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?annotation", nil),
		},
		{
			name: "GetObjectAttributes",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?attributes", nil),
		},
		{
			name: "GetObjectLegalHold",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?legal-hold", nil),
		},
		{
			name: "GetObjectLockConfiguration",
			req:  httptest.NewRequest(http.MethodGet, "/test?object-lock", nil),
		},
		{
			name: "GetObjectRetention",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?retention", nil),
		},
		{
			name: "GetObjectTagging",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?tagging", nil),
		},
		{
			name: "GetObjectTorrent",
			req:  httptest.NewRequest(http.MethodGet, "/test/test?torrent", nil),
		},
		{
			name: "GetPublicAccessBlock",
			req:  httptest.NewRequest(http.MethodGet, "/test?publicAccessBlock", nil),
		},
		{
			name: "HeadBucket",
			req:  httptest.NewRequest(http.MethodHead, "/test", nil),
		},
		{
			name: "ListBucketAnalyticsConfigurations",
			req:  httptest.NewRequest(http.MethodGet, "/test?analytics", nil),
		},
		{
			name: "ListBucketIntelligentTieringConfigurations",
			req:  httptest.NewRequest(http.MethodGet, "/test?intelligent-tiering", nil),
		},
		{
			name: "ListBucketInventoryConfigurations",
			req:  httptest.NewRequest(http.MethodGet, "/test?inventory", nil),
		},
		{
			name: "ListBucketMetricsConfigurations",
			req:  httptest.NewRequest(http.MethodGet, "/test?metrics", nil),
		},
		{
			name: "ListBuckets",
			req:  httptest.NewRequest(http.MethodGet, "/", nil),
		},
		{
			name: "ListDirectoryBuckets",
			req:  httptest.NewRequest(http.MethodGet, "/", nil),
		},
		{
			name: "ListObjectAnnotations",
			req:  httptest.NewRequest(http.MethodGet, "/test?annotations", nil),
		},
		{
			name: "ListObjectVersions",
			req:  httptest.NewRequest(http.MethodGet, "/test?versions", nil),
		},
		{
			name: "PutBucketAbac",
			req:  httptest.NewRequest(http.MethodPut, "/test?abac", nil),
		},
		{
			name: "PutBucketAccelerateConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?accelerate", nil),
		},
		{
			name: "PutBucketAcl",
			req:  httptest.NewRequest(http.MethodPut, "/test?acl", nil),
		},
		{
			name: "PutBucketAnalyticsConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?analytics&id=id", nil),
		},
		{
			name: "PutBucketCors",
			req:  httptest.NewRequest(http.MethodPut, "/test?cors", nil),
		},
		{
			name: "PutBucketEncryption",
			req:  httptest.NewRequest(http.MethodPut, "/test?encryption", nil),
		},
		{
			name: "PutBucketIntelligentTieringConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?intelligent-tiering&id=id", nil),
		},
		{
			name: "PutBucketInventoryConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?inventory&id=id", nil),
		},
		{
			name: "PutBucketLifecycle",
			req:  httptest.NewRequest(http.MethodPut, "/test?lifecycle", nil),
		},
		{
			name: "PutBucketLifecycleConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?lifecycle", nil),
		},
		{
			name: "PutBucketLogging",
			req:  httptest.NewRequest(http.MethodPut, "/test?logging", nil),
		},
		{
			name: "PutBucketMetricsConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?metrics&id=id", nil),
		},
		{
			name: "PutBucketNotification",
			req:  httptest.NewRequest(http.MethodPut, "/test?notification", nil),
		},
		{
			name: "PutBucketNotificationConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?notification", nil),
		},
		{
			name: "PutBucketOwnershipControls",
			req:  httptest.NewRequest(http.MethodPut, "/test?ownershipControls", nil),
		},
		{
			name: "PutBucketPolicy",
			req:  httptest.NewRequest(http.MethodPut, "/test?policy", nil),
		},
		{
			name: "PutBucketReplication",
			req:  httptest.NewRequest(http.MethodPut, "/test?replication", nil),
		},
		{
			name: "PutBucketRequestPayment",
			req:  httptest.NewRequest(http.MethodPut, "/test?requestPayment", nil),
		},
		{
			name: "PutBucketTagging",
			req:  httptest.NewRequest(http.MethodPut, "/test?tagging", nil),
		},
		{
			name: "PutBucketVersioning",
			req:  httptest.NewRequest(http.MethodPut, "/test?versioning", nil),
		},
		{
			name: "PutBucketWebsite",
			req:  httptest.NewRequest(http.MethodPut, "/test?website", nil),
		},

		{
			name: "PutObjectAcl",
			req:  httptest.NewRequest(http.MethodPut, "/test/test?acl", nil),
		},
		{
			name: "PutObjectAnnotation",
			req:  httptest.NewRequest(http.MethodPut, "/test/test?annotation", nil),
		},
		{
			name: "PutObjectLegalHold",
			req:  httptest.NewRequest(http.MethodPut, "/test/test?legal-hold", nil),
		},
		{
			name: "PutObjectLockConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?object-lock", nil),
		},
		{
			name: "PutObjectRetention",
			req:  httptest.NewRequest(http.MethodPut, "/test/test?retention", nil),
		},
		{
			name: "PutObjectTagging",
			req:  httptest.NewRequest(http.MethodPut, "/test/test?tagging", nil),
		},
		{
			name: "PutPublicAccessBlock",
			req:  httptest.NewRequest(http.MethodPut, "/test?publicAccessBlock", nil),
		},
		{
			name: "RenameObject",
			req: func() *http.Request {
				req := httptest.NewRequest(http.MethodPut, "/test/test", nil)
				req.Header.Set("x-amz-rename-source", "/test/source")

				return req
			}(),
		},
		{
			name: "RestoreObject",
			req:  httptest.NewRequest(http.MethodPost, "/test/test?restore", nil),
		},
		{
			name: "SelectObjectContent",
			req:  httptest.NewRequest(http.MethodPost, "/test/test?select&select-type=2", nil),
		},
		{
			name: "UpdateBucketMetadataAnnotationTableConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?metadataTable", nil),
		},
		{
			name: "UpdateBucketMetadataInventoryTableConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?metadataTable", nil),
		},
		{
			name: "UpdateBucketMetadataJournalTableConfiguration",
			req:  httptest.NewRequest(http.MethodPut, "/test?metadataTable", nil),
		},
		{
			name: "UpdateObjectEncryption",
			req:  httptest.NewRequest(http.MethodPost, "/test/test?encryption", nil),
		},
		{
			name: "UploadPartCopy",
			req: func() *http.Request {
				req := httptest.NewRequest(
					http.MethodPut,
					"/test/test?partNumber=1&uploadId=upload-id",
					nil,
				)
				req.Header.Set("x-amz-copy-source", "/from/source")

				return req
			}(),
		},
		{
			name: "WriteGetObjectResponse",
			req:  httptest.NewRequest(http.MethodPost, "/WriteGetObjectResponse", nil),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ma := &mockAuthenticator{}
			testToken := jwt.New()
			_ = testToken.Set("sub", "unit_test_user")
			ma.On("Authenticate", mock.Anything).Once().Return(testToken, nil)

			p := &proxy{
				auth: ma,
			}

			w := httptest.NewRecorder()
			p.ServeHTTP(w, tc.req)

			assert.Equal(t, 403, w.Code)
			ma.AssertExpectations(t)
		})
	}
}

type mockServer struct {
	mock.Mock
}

func (s *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	args := s.Called(r.Method, r.URL.Path, r.URL.RawQuery)

	if headers := args.Get(2); headers != nil {
		for k, v := range headers.(map[string]string) {
			w.Header().Set(k, v)
		}
	}

	w.WriteHeader(args.Int(0))
	if body := args.Get(1); body != nil {
		_, _ = w.Write(body.([]byte)) // #nosec G705 -- request controlled by unit test
	}
}

type mockAuthenticator struct {
	mock.Mock
}

func (m *mockAuthenticator) Authenticate(r *http.Request) (jwt.Token, error) {
	args := m.Called(r.Header.Get("Authorization"))

	token := args.Get(0)
	if token == nil {
		return nil, args.Error(1)
	}

	return token.(jwt.Token), args.Error(1)
}
