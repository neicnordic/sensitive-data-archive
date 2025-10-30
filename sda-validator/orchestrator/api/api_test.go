package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api/openapi_interface"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type ValidatorAPITestSuite struct {
	suite.Suite

	tempDir        string
	ginEngine      *gin.Engine
	httpTestServer *httptest.Server

	mockDatabase *mockDatabase
	mockBroker   *mockBroker
}

func (ts *ValidatorAPITestSuite) SetupSuite() {
	ts.mockDatabase = &mockDatabase{}
	ts.mockBroker = &mockBroker{}

	ts.httpTestServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		switch req.RequestURI {
		case "/users/test_user/files", "/users/different_user/files":
			// Set the response status code
			w.WriteHeader(http.StatusOK)
			// Set the response body
			_, _ = w.Write([]byte(`[
{
	"FileID": "test-file-id-1",
	"InboxPath": "testFile1",
	"submissionFileSize": 1024
},{
	"FileID": "test-file-id-2",
	"InboxPath": "testFile2",
	"submissionFileSize": 1024
},{
	"FileID": "test-file-id-3",
	"InboxPath": "testFile3",
	"submissionFileSize": 1024
},{
	"FileID": "test-file-id-4",
	"InboxPath": "test_dir/testFile4",
	"submissionFileSize": 1024
},{
	"FileID": "test-file-id-5",
	"InboxPath": "test_dir/testFile5",
    "submissionFileSize": 1024
}
]`))
		default:
			// Set the response status code
			w.WriteHeader(http.StatusInternalServerError)
			// Set the response body
			_, _ = fmt.Fprint(w, "unexpected path called")
		}
	}))

	database.RegisterDatabase(ts.mockDatabase)

	ts.ginEngine = gin.Default()
	ts.ginEngine.Use(mockAuthenticator)

	ts.ginEngine = openapi.NewRouterWithGinEngine(ts.ginEngine,
		openapi.ApiHandleFunctions{
			ValidatorOrchestratorAPI: &validatorAPIImpl{
				sdaApiUrl:                     ts.httpTestServer.URL,
				sdaApiToken:                   "mock-sdaApiToken",
				validationFileSizeLimit:       1024 * 4,
				validationJobPreparationQueue: "job-preparation-queue",
				broker:                        ts.mockBroker,
			}})

	validators.Validators = map[string]*validators.ValidatorDescription{
		"mock-validator": {
			ValidatorId:       "mock-validator",
			Name:              "mock validator",
			Description:       "Validator for mocking",
			Version:           "v0.0.0",
			Mode:              "file",
			PathSpecification: nil,
			ValidatorPath:     "/mock-validator.sif",
		},
	}
}

func (ts *ValidatorAPITestSuite) SetupTest() {
	ts.tempDir = ts.T().TempDir()
	// Reset any Asserts and On() on mocks from previous tests
	*ts.mockDatabase = mockDatabase{}
	*ts.mockBroker = mockBroker{}
}

func (ts *ValidatorAPITestSuite) TearDownSuite() {
	ts.httpTestServer.Close()
}

func TestValidatorAPITestSuite(t *testing.T) {
	suite.Run(t, new(ValidatorAPITestSuite))
}

type mockDatabase struct {
	mock.Mock
}

func (m *mockDatabase) Commit() error {
	_ = m.Called()
	return nil
}

func (m *mockDatabase) Rollback() error {
	_ = m.Called()
	return nil
}

func (m *mockDatabase) BeginTransaction(_ context.Context) (database.Transaction, error) {
	_ = m.Called()
	return m, nil
}

func (m *mockDatabase) Close() error {
	_ = m.Called()
	return nil
}

func (m *mockDatabase) ReadValidationResult(_ context.Context, validationID string, userID *string) (*model.ValidationResult, error) {
	args := m.Called(validationID, userID)
	return args.Get(0).(*model.ValidationResult), args.Error(1)
}

func (m *mockDatabase) ReadValidationInformation(_ context.Context, _ string) (*model.ValidationInformation, error) {
	// Function not needed for unit test, but to implement interface
	panic("database.ReadValidationInformation call not expected in unit tests")
}

func (m *mockDatabase) InsertFileValidationJob(_ context.Context, validationID, validatorID, fileID, filePath string, fileSubmissionSize int64, submissionUser, triggeredBy string, startedAt time.Time) error {
	args := m.Called(validationID, validatorID, fileID, filePath, fileSubmissionSize, submissionUser, triggeredBy, startedAt.Format(time.RFC3339))
	return args.Error(0)
}

func (m *mockDatabase) UpdateFileValidationJob(_ context.Context, _, _, _, _ string, _ []*model.Message, _ time.Time, _ string, _ []*model.Message) error {
	// Function not needed for unit test, but to implement interface
	panic("database.UpdateFileValidationJob call not expected in unit tests")
}

func (m *mockDatabase) AllValidationJobsDone(_ context.Context, _ string) (bool, error) {
	// Function not needed for unit test, but to implement interface
	panic("database.AllValidationJobsDone call not expected in unit tests")
}

type mockBroker struct {
	mock.Mock
}

func (m *mockBroker) PublishMessage(_ context.Context, destination string, body []byte) error {
	args := m.Called(destination, body)
	return args.Error(0)
}

func (m *mockBroker) Subscribe(_ context.Context, queue, consumerID string, handleFunc func(context.Context, amqp.Delivery) error) error {
	// Function not needed for unit test, but to implement interface
	panic("broker.subscribe call not expected in unit tests")
}

func (m *mockBroker) Close() error {
	// Function not needed for unit test, but to implement interface
	panic("broker.close call not expected in unit tests")
}

func (m *mockBroker) ConnectionWatcher() chan *amqp.Error {
	// Function not needed for unit test, but to implement interface
	panic("broker.ConnectionWatcher call not expected in unit tests")
}

func mockAuthenticator(c *gin.Context) {
	token := jwt.New()
	if err := token.Set("sub", "test_user"); err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Set("token", token)
}

func (ts *ValidatorAPITestSuite) TestNewValidatorAPIImpl() {
	impl, err := NewValidatorAPIImpl(
		SdaApiUrl("mock-url"),
		SdaApiToken("mock-token"),
		ValidationFileSizeLimit(1),
		ValidationJobPreparationQueue("mock-queue"),
		Broker(ts.mockBroker),
	)
	ts.NoError(err)
	ts.NotNil(impl)
}
func (ts *ValidatorAPITestSuite) TestInitWorkers_NoSdaApiUrl() {
	impl, err := NewValidatorAPIImpl(
		SdaApiToken("mock-token"),
		ValidationFileSizeLimit(1),
		ValidationJobPreparationQueue("mock-queue"),
		Broker(ts.mockBroker),
	)
	ts.EqualError(err, "sdaApiUrl is required")
	ts.Nil(impl)
}
func (ts *ValidatorAPITestSuite) TestInitWorkers_NoSdaApiToken() {
	impl, err := NewValidatorAPIImpl(
		SdaApiUrl("mock-url"),
		ValidationFileSizeLimit(1),
		ValidationJobPreparationQueue("mock-queue"),
		Broker(ts.mockBroker),
	)
	ts.EqualError(err, "sdaApiToken is required")
	ts.Nil(impl)
}
func (ts *ValidatorAPITestSuite) TestInitWorkers_NoValidationFileSizeLimit() {
	impl, err := NewValidatorAPIImpl(
		SdaApiUrl("mock-url"),
		SdaApiToken("mock-token"),
		ValidationJobPreparationQueue("mock-queue"),
		Broker(ts.mockBroker),
	)
	ts.EqualError(err, "validationFileSizeLimit is required")
	ts.Nil(impl)
}
func (ts *ValidatorAPITestSuite) TestInitWorkers_NoValidationJobPreparationQueue() {
	impl, err := NewValidatorAPIImpl(
		SdaApiUrl("mock-url"),
		SdaApiToken("mock-token"),
		ValidationFileSizeLimit(1),
		Broker(ts.mockBroker),
	)
	ts.EqualError(err, "validationJobPreparationQueue is required")
	ts.Nil(impl)
}
func (ts *ValidatorAPITestSuite) TestInitWorkers_NoBroker() {
	impl, err := NewValidatorAPIImpl(
		SdaApiUrl("mock-url"),
		SdaApiToken("mock-token"),
		ValidationFileSizeLimit(1),
		ValidationJobPreparationQueue("mock-queue"),
	)
	ts.EqualError(err, "broker is required")
	ts.Nil(impl)
}

func (ts *ValidatorAPITestSuite) TestValidatePost_MissingValidator() {
	w := httptest.NewRecorder()
	body, err := json.Marshal(&openapi.ValidateRequest{
		FilePaths:  []string{"123", "abc"},
		Validators: []string{"abc-validator"},
	})
	if err != nil {
		ts.FailNow(err.Error(), "failed to prepare validate request")
	}
	req, _ := http.NewRequest("POST", "/validate", bytes.NewReader(body))
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusBadRequest, w.Code)
	ts.Equal(`{"error":"[abc-validator] are not supported validators"}`, w.Body.String())
}

func (ts *ValidatorAPITestSuite) TestValidatePost_FilesNotFound() {
	w := httptest.NewRecorder()
	body, err := json.Marshal(&openapi.ValidateRequest{
		FilePaths:  []string{"testFile1", "testFile2", "test_dir/testFile8", "testFile10"},
		Validators: []string{"mock-validator"},
	})
	if err != nil {
		ts.FailNow(err.Error(), "failed to prepare validate request")
	}
	req, _ := http.NewRequest("POST", "/validate", bytes.NewReader(body))
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusBadRequest, w.Code)
	ts.Equal(`{"error":"files: [test_dir/testFile8 testFile10] not found"}`, w.Body.String())
}

func (ts *ValidatorAPITestSuite) TestValidatePost_ExceedValidationFileSizeLimit() {
	w := httptest.NewRecorder()
	body, err := json.Marshal(&openapi.ValidateRequest{
		FilePaths:  []string{"testFile1", "testFile2", "testFile3", "test_dir/testFile4", "test_dir/testFile5"},
		Validators: []string{"mock-validator"},
	})
	if err != nil {
		ts.FailNow(err.Error(), "failed to prepare validate request")
	}
	req, _ := http.NewRequest("POST", "/validate", bytes.NewReader(body))
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusBadRequest, w.Code)
	ts.Equal(`{"error":"requested files exceed the file size limit"}`, w.Body.String())
}
func (ts *ValidatorAPITestSuite) TestAdminValidatePost_NoTokenInContext() {
	ginEngine := openapi.NewRouter(openapi.ApiHandleFunctions{
		ValidatorOrchestratorAPI: &validatorAPIImpl{
			sdaApiUrl:                     ts.httpTestServer.URL,
			sdaApiToken:                   "mock-sdaApiToken",
			validationFileSizeLimit:       1024 * 4,
			validationJobPreparationQueue: "job-preparation-queue",
			broker:                        ts.mockBroker,
		}})

	ts.ginEngine.Use(func(c *gin.Context) {
		c.Set("token", nil)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/validate", nil)
	ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusInternalServerError, w.Code)
}

func (ts *ValidatorAPITestSuite) TestValidatePost_NoTokenInContext() {
	ginEngine := openapi.NewRouter(openapi.ApiHandleFunctions{
		ValidatorOrchestratorAPI: &validatorAPIImpl{
			sdaApiUrl:                     ts.httpTestServer.URL,
			sdaApiToken:                   "mock-sdaApiToken",
			validationFileSizeLimit:       1024 * 4,
			validationJobPreparationQueue: "job-preparation-queue",
			broker:                        ts.mockBroker,
		}})

	ts.ginEngine.Use(func(c *gin.Context) {
		c.Set("token", nil)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/validate", nil)
	ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusInternalServerError, w.Code)
}

func (ts *ValidatorAPITestSuite) TestValidatePost() {
	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("InsertFileValidationJob", mock.Anything, "mock-validator", mock.Anything, mock.Anything, int64(1024), "test_user", "test_user", mock.Anything).Return(nil)
	ts.mockBroker.On("PublishMessage", "job-preparation-queue", mock.Anything).Return(nil)

	w := httptest.NewRecorder()
	body, err := json.Marshal(&openapi.ValidateRequest{
		FilePaths:  []string{"testFile1", "testFile2", "test_dir/testFile5"},
		Validators: []string{"mock-validator"},
	})
	if err != nil {
		ts.FailNow(err.Error(), "failed to prepare validate request")
	}
	req, _ := http.NewRequest("POST", "/validate", bytes.NewReader(body))
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)

	validateResult := new(openapi.ValidatePost200Response)
	if err := json.Unmarshal(w.Body.Bytes(), validateResult); err != nil {
		ts.FailNow(err.Error(), "failed to parse response body to ValidatePost200Response")
	}
	if _, err := uuid.Parse(validateResult.ValidationId); err != nil {
		ts.FailNow(err.Error(), "failed to parse validation id as uuid in response")
	}

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "InsertFileValidationJob", 3)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-1", "testFile1", int64(1024), "test_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-2", "testFile2", int64(1024), "test_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-5", "test_dir/testFile5", int64(1024), "test_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "Commit", 1)
	ts.mockDatabase.AssertNotCalled(ts.T(), "Rollback", 1) // We expect rollback to have been called given its deferred to ensure tx is closed

	ts.mockBroker.AssertCalled(ts.T(), "PublishMessage", "job-preparation-queue", mock.Anything)
}

func (ts *ValidatorAPITestSuite) TestAdminValidatePost() {
	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("InsertFileValidationJob", mock.Anything, "mock-validator", mock.Anything, mock.Anything, int64(1024), "different_user", "test_user", mock.Anything).Return(nil)
	ts.mockBroker.On("PublishMessage", "job-preparation-queue", mock.Anything).Return(nil)

	w := httptest.NewRecorder()
	body, err := json.Marshal(&openapi.AdminValidateRequest{
		FilePaths:  []string{"testFile2", "test_dir/testFile4", "test_dir/testFile5"},
		Validators: []string{"mock-validator"},
		UserId:     "different_user",
	})
	if err != nil {
		ts.FailNow(err.Error(), "failed to prepare validate request")
	}
	req, _ := http.NewRequest("POST", "/admin/validate", bytes.NewReader(body))
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)

	validateResult := new(openapi.ValidatePost200Response)
	if err := json.Unmarshal(w.Body.Bytes(), validateResult); err != nil {
		ts.FailNow(err.Error(), "failed to parse response body to ValidatePost200Response")
	}
	if _, err := uuid.Parse(validateResult.ValidationId); err != nil {
		ts.FailNow(err.Error(), "failed to parse validation id as uuid in response")
	}

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "InsertFileValidationJob", 3)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-4", "test_dir/testFile4", int64(1024), "different_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-2", "testFile2", int64(1024), "different_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "InsertFileValidationJob", mock.Anything, "mock-validator", "test-file-id-5", "test_dir/testFile5", int64(1024), "different_user", "test_user", mock.Anything)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "Commit", 1)
	ts.mockDatabase.AssertNotCalled(ts.T(), "Rollback", 1) // We expect rollback to have been called given its deferred to ensure tx is closed

	ts.mockBroker.AssertCalled(ts.T(), "PublishMessage", "job-preparation-queue", mock.Anything)
}

func (ts *ValidatorAPITestSuite) TestAdminResultGet() {
	startedAt, _ := time.Parse(time.RFC3339, time.Now().Add(-2*time.Second).Format(time.RFC3339))
	finishedAt, _ := time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
	testValidationResult := &model.ValidationResult{
		ValidationID: uuid.NewString(),
		ValidatorResults: []*model.ValidatorResult{
			{
				ValidatorID: "mock-validator",
				Result:      "passed",
				StartedAt:   startedAt,
				FinishedAt:  finishedAt,
				Messages:    nil,
				Files: []*model.FileResult{
					{
						FilePath: "testFile1",
						Result:   "passed",
						Messages: nil,
					}, {
						FilePath: "testFile2",
						Result:   "passed",
						Messages: nil,
					}, {
						FilePath: "test_dir/testFile5",
						Result:   "passed",
						Messages: nil,
					},
				},
			},
		},
	}

	ts.mockDatabase.On("ReadValidationResult", testValidationResult.ValidationID, (*string)(nil)).Return(testValidationResult, nil)

	w := httptest.NewRecorder()

	req, _ := http.NewRequest("GET", fmt.Sprintf("/admin/result?validation_id=%s", testValidationResult.ValidationID), nil)
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)

	var resultResponse []openapi.ResultResponseInner
	if err := json.Unmarshal(w.Body.Bytes(), &resultResponse); err != nil {
		ts.FailNow(err.Error(), "failed to parse response body to []openapi.ResultResponseInner")
	}

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "ReadValidationResult", 1)
	ts.mockDatabase.AssertCalled(ts.T(), "ReadValidationResult", testValidationResult.ValidationID, (*string)(nil))

	ts.Equal(len(testValidationResult.ValidatorResults), len(resultResponse))

	ts.Equal(testValidationResult.ValidatorResults[0].Result, resultResponse[0].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].ValidatorID, resultResponse[0].ValidatorId)
	ts.Equal(testValidationResult.ValidatorResults[0].StartedAt, resultResponse[0].StartedAt)
	ts.Equal(testValidationResult.ValidatorResults[0].FinishedAt, resultResponse[0].FinishedAt)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Messages), len(resultResponse[0].Messages))
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files), len(resultResponse[0].Files))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[0].Result, resultResponse[0].Files[0].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[0].FilePath, resultResponse[0].Files[0].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[0].Messages), len(resultResponse[0].Files[0].Messages))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[1].Result, resultResponse[0].Files[1].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[1].FilePath, resultResponse[0].Files[1].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[1].Messages), len(resultResponse[0].Files[1].Messages))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[2].Result, resultResponse[0].Files[2].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[2].FilePath, resultResponse[0].Files[2].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[2].Messages), len(resultResponse[0].Files[2].Messages))
}

func (ts *ValidatorAPITestSuite) TestResultGet_NoTokenInContext() {
	ginEngine := openapi.NewRouter(openapi.ApiHandleFunctions{
		ValidatorOrchestratorAPI: &validatorAPIImpl{
			sdaApiUrl:                     ts.httpTestServer.URL,
			sdaApiToken:                   "mock-sdaApiToken",
			validationFileSizeLimit:       1024 * 4,
			validationJobPreparationQueue: "job-preparation-queue",
			broker:                        ts.mockBroker,
		}})

	ts.ginEngine.Use(func(c *gin.Context) {
		c.Set("token", nil)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/result", nil)
	ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusInternalServerError, w.Code)
}

func (ts *ValidatorAPITestSuite) TestResultGet() {

	startedAt, _ := time.Parse(time.RFC3339, time.Now().Add(-2*time.Second).Format(time.RFC3339))
	finishedAt, _ := time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
	testValidationResult := &model.ValidationResult{
		ValidationID: uuid.NewString(),
		ValidatorResults: []*model.ValidatorResult{
			{
				ValidatorID: "mock-validator",
				Result:      "failed",
				StartedAt:   startedAt,
				FinishedAt:  finishedAt,
				Messages: []*model.Message{
					{
						Level:   "INFO",
						Time:    time.Now().Format(time.RFC3339),
						Message: "All files passed",
					},
				},
				Files: []*model.FileResult{
					{
						FilePath: "testFile1",
						Result:   "passed",
						Messages: nil,
					}, {
						FilePath: "testFile2",
						Result:   "passed",
						Messages: nil,
					}, {
						FilePath: "test_dir/testFile5",
						Result:   "failed",
						Messages: []*model.Message{
							{
								Level:   "error",
								Time:    time.Now().Format(time.RFC3339),
								Message: "file failed validation",
							},
						},
					},
				},
			},
		},
	}

	testUser := "test_user"
	ts.mockDatabase.On("ReadValidationResult", testValidationResult.ValidationID, &testUser).Return(testValidationResult, nil)

	w := httptest.NewRecorder()

	req, _ := http.NewRequest("GET", fmt.Sprintf("/result?validation_id=%s", testValidationResult.ValidationID), nil)
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)

	var resultResponse []openapi.ResultResponseInner
	if err := json.Unmarshal(w.Body.Bytes(), &resultResponse); err != nil {
		ts.FailNow(err.Error(), "failed to parse response body to []openapi.ResultResponseInner")
	}

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "ReadValidationResult", 1)
	ts.mockDatabase.AssertCalled(ts.T(), "ReadValidationResult", testValidationResult.ValidationID, &testUser)

	ts.Equal(len(testValidationResult.ValidatorResults), len(resultResponse))

	ts.Equal(testValidationResult.ValidatorResults[0].Result, resultResponse[0].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].ValidatorID, resultResponse[0].ValidatorId)
	ts.Equal(testValidationResult.ValidatorResults[0].StartedAt, resultResponse[0].StartedAt)
	ts.Equal(testValidationResult.ValidatorResults[0].FinishedAt, resultResponse[0].FinishedAt)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Messages), len(resultResponse[0].Messages))
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files), len(resultResponse[0].Files))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[0].Result, resultResponse[0].Files[0].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[0].FilePath, resultResponse[0].Files[0].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[0].Messages), len(resultResponse[0].Files[0].Messages))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[1].Result, resultResponse[0].Files[1].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[1].FilePath, resultResponse[0].Files[1].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[1].Messages), len(resultResponse[0].Files[1].Messages))
	ts.Equal(testValidationResult.ValidatorResults[0].Files[2].Result, resultResponse[0].Files[2].Result)
	ts.Equal(testValidationResult.ValidatorResults[0].Files[2].FilePath, resultResponse[0].Files[2].Path)
	ts.Equal(len(testValidationResult.ValidatorResults[0].Files[2].Messages), len(resultResponse[0].Files[2].Messages))

}

func (ts *ValidatorAPITestSuite) TestValidatorsGet() {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/validators", nil)
	ts.ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)
	ts.Equal(`["mock-validator"]`, w.Body.String())
}
