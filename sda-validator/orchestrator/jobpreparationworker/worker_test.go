package jobpreparationworker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type JobPreparationWorkerTestSuite struct {
	suite.Suite

	tempDir        string
	httpTestServer *httptest.Server

	mockDatabase *mockDatabase
	mockBroker   *mockBroker
}

func (ts *JobPreparationWorkerTestSuite) SetupSuite() {
	ts.httpTestServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.RequestURI, "/users/test_user/file/"):
			publicKey, err := base64.StdEncoding.DecodeString(req.Header.Get("C4GH-Public-Key"))
			if err != nil || len(publicKey) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, "bad public key")

				return
			}

			reader := bytes.NewReader(publicKey)
			newReaderPublicKey, err := keys.ReadPublicKey(reader)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, "could not read public key")

				return
			}

			encryptedFile := bytes.Buffer{}
			encryptedFileWriter, err := streaming.NewCrypt4GHWriter(&encryptedFile, [32]byte{}, [][32]byte{newReaderPublicKey}, nil)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, "could not read create crypt4gh writer")

				return
			}

			file := bytes.Buffer{}
			_, _ = file.Write([]byte(fmt.Sprintf("this is file: %s", filepath.Base(req.URL.Path))))
			_, _ = io.Copy(encryptedFileWriter, &file)

			_ = encryptedFileWriter.Close()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(encryptedFile.Bytes())
		default:
			// Set the response status code
			w.WriteHeader(http.StatusInternalServerError)
			// Set the response body
			_, _ = fmt.Fprint(w, "unexpected path called")
		}
	}))

	validators.Validators = map[string]*validators.ValidatorDescription{
		"mock-validator": {
			ValidatorID:       "mock-validator",
			Name:              "mock validator",
			Description:       "Validator for mocking",
			Version:           "v0.0.0",
			Mode:              "file",
			PathSpecification: nil,
			ValidatorPath:     "/mock-validator.sif",
		},
	}
}
func (ts *JobPreparationWorkerTestSuite) SetupTest() {
	ts.tempDir = ts.T().TempDir()
	// Reset any Asserts and On() on mocks from previous tests
	ts.mockDatabase = &mockDatabase{}
	ts.mockBroker = &mockBroker{}
	database.RegisterDatabase(ts.mockDatabase)
}

func (ts *JobPreparationWorkerTestSuite) TearDownSuite() {
	ts.httpTestServer.Close()
}

func TestJobPreparationWorkerTestSuite(t *testing.T) {
	suite.Run(t, new(JobPreparationWorkerTestSuite))
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
	// Function not needed for unit test, but to implement interface
	panic("database.ReadValidationResult call not expected in unit tests")
}

func (m *mockDatabase) ReadValidationInformation(_ context.Context, validationID string) (*model.ValidationInformation, error) {
	args := m.Called(validationID)

	return args.Get(0).(*model.ValidationInformation), args.Error(1)
}

func (m *mockDatabase) InsertFileValidationJob(_ context.Context, _ *model.InsertFileValidationJobParameters) error {
	// Function not needed for unit test, but to implement interface
	panic("database.InsertFileValidationJob call not expected in unit tests")
}

func (m *mockDatabase) UpdateFileValidationJob(_ context.Context, _ *model.UpdateFileValidationJobParameters) error {
	// Function not needed for unit test, but to implement interface
	panic("database.UpdateFileValidationJob call not expected in unit tests")
}

func (m *mockDatabase) AllValidationJobsDone(_ context.Context, _ string) (bool, error) {
	// Function not needed for unit test, but to implement interface
	panic("database.AllValidationJobsDone call not expected in unit tests")
}

func (m *mockDatabase) UpdateAllValidationJobFilesOnError(ctx context.Context, validationID string, validatorMessage *model.Message) error {
	args := m.Called(validationID, validatorMessage)

	return args.Error(0)
}

type mockBroker struct {
	mock.Mock
	messageChans map[string]chan amqp.Delivery
}

func (m *mockBroker) PublishMessage(_ context.Context, destination string, body []byte) error {
	args := m.Called(destination, body)

	return args.Error(0)
}

func (m *mockBroker) Subscribe(ctx context.Context, queue, consumerID string, handleFunc func(context.Context, amqp.Delivery) error) error {
	args := m.Called(queue, consumerID)

	if err := args.Error(0); err != nil {
		return err
	}

	messageChan, ok := m.messageChans[consumerID]
	if !ok {
		return nil
	}
	for {
		select {
		case msg, ok := <-messageChan:
			if !ok {
				return nil
			}
			if err := handleFunc(context.TODO(), msg); err != nil {
				return errors.Join(errors.New("unexpected consumer handleFunc error"), err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *mockBroker) Close() error {
	// Function not needed for unit test, but to implement interface
	panic("broker.close call not expected in unit tests")
}

func (m *mockBroker) Monitor() chan *amqp.Error {
	// Function not needed for unit test, but to implement interface
	panic("broker.Monitor call not expected in unit tests")
}

func (ts *JobPreparationWorkerTestSuite) TestInitWorkers() {
	ts.mockBroker.On("Subscribe", "job-preparation-queue", mock.Anything).Return(nil)
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.NoError(err)
	ts.Len(workers.workers, 2)
	workers.Shutdown()
}

func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoValidationWorkDirectory() {
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		WorkerCount(2),
	)
	ts.EqualError(err, "validationWorkDir is required")
	ts.Nil(workers)
}
func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoBroker() {
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.EqualError(err, "broker is required")
	ts.Nil(workers)
}
func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoSdaApiToken() {
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.EqualError(err, "sdaAPIToken is required")
	ts.Nil(workers)
}

func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoSdaApiUrl() {
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.EqualError(err, "sdaAPIURL is required")
	ts.Nil(workers)
}

func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoDestinationQueue() {
	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.EqualError(err, "destinationQueue is required")
	ts.Nil(workers)
}

func (ts *JobPreparationWorkerTestSuite) TestInitWorkers_NoSourceQueue() {
	workers, err := NewWorkers(
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	ts.EqualError(err, "sourceQueue is required")
	ts.Nil(workers)
}

func (ts *JobPreparationWorkerTestSuite) TestStartWorkers_NoInit() {
	workers := &Workers{}
	select {
	case <-time.After(2 * time.Second):
		ts.FailNow("timeout error, expected MonitorWorker to return error")
	case err := <-workers.Monitor():
		ts.EqualError(err, "workers have not been initialized")
	}
}

func (ts *JobPreparationWorkerTestSuite) TestStartWorkers_SubscribeError() {
	ts.mockBroker.On("Subscribe", "job-preparation-queue", mock.Anything).Return(errors.New("subscribe error"))

	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	select {
	case <-time.After(2 * time.Second):
		ts.FailNow("timeout error, expected MonitorWorker to return error")
	case err := <-workers.Monitor():
		ts.EqualError(err, "subscribe error")
	}
	workers.Shutdown()
}

func (ts *JobPreparationWorkerTestSuite) TestStartAndShutdownWorkers() {
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-preparation-worker-0": make(chan amqp.Delivery),
		"job-preparation-worker-1": make(chan amqp.Delivery),
	}
	ts.mockBroker.On("Subscribe", "job-preparation-queue", mock.Anything).Return(nil)

	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers.workers, 2)

	for i, worker := range workers.workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-preparation-worker-%d", i), worker.id)
	}

	workers.Shutdown()

	for _, worker := range workers.workers {
		ts.Equal(false, worker.running)
	}
}

func (ts *JobPreparationWorkerTestSuite) TestWorkersConsume() {
	worker1MessageChan := make(chan amqp.Delivery)
	worker2MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-preparation-worker-0": worker1MessageChan,
		"job-preparation-worker-1": worker2MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-preparation-queue", mock.Anything).Return(nil)

	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(ts.httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(2),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers.workers, 2)

	ts.mockBroker.On("PublishMessage", "job-queue", mock.Anything).Return(nil)

	for i, worker := range workers.workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-preparation-worker-%d", i), worker.id)
	}

	validationInformation1 := &model.ValidationInformation{
		ValidationID:     uuid.NewString(),
		ValidatorIDs:     []string{"mock-validator"},
		SubmissionUserID: "test_user",
		Files: []*model.FileInformation{
			{
				FileID:             "testFileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId2",
				FilePath:           "another_dir/file2",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId3",
				FilePath:           "file3",
				SubmissionFileSize: 1,
			},
		},
	}
	ts.mockDatabase.On("ReadValidationInformation", validationInformation1.ValidationID).Return(validationInformation1, nil)

	validationInformation2 := &model.ValidationInformation{
		ValidationID:     uuid.NewString(),
		ValidatorIDs:     []string{"mock-validator"},
		SubmissionUserID: "test_user",
		Files: []*model.FileInformation{
			{
				FileID:             "testFileId11",
				FilePath:           "test_dir/file11",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId21",
				FilePath:           "another_dir/file21",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId31",
				FilePath:           "file31",
				SubmissionFileSize: 1,
			},
		},
	}
	ts.mockDatabase.On("ReadValidationInformation", validationInformation2.ValidationID).Return(validationInformation2, nil)

	message1, err := json.Marshal(&model.JobPreparationMessage{ValidationID: validationInformation1.ValidationID})
	if err != nil {
		ts.FailNow("failed to marshal job preparation message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message1,
	}

	message2, err := json.Marshal(&model.JobPreparationMessage{ValidationID: validationInformation2.ValidationID})
	if err != nil {
		ts.FailNow("failed to marshal job preparation message", err)
	}
	worker2MessageChan <- amqp.Delivery{
		Body: message2,
	}

	workers.Shutdown()

	for _, worker := range workers.workers {
		ts.Equal(false, worker.running)
	}

	for _, file := range validationInformation1.Files {
		fileContent, err := os.ReadFile(filepath.Join(ts.tempDir, validationInformation1.ValidationID, "files", file.FilePath))
		if err != nil {
			ts.Failf("failed to read file: %s, due to: %v", file.FilePath, err)
		}
		ts.Equal(fmt.Sprintf("this is file: %s", file.FileID), string(fileContent))
	}

	for _, file := range validationInformation2.Files {
		fileContent, err := os.ReadFile(filepath.Join(ts.tempDir, validationInformation2.ValidationID, "files", file.FilePath))
		if err != nil {
			ts.Failf(err.Error(), "failed to read file: %s, due to: %v", file.FilePath, err)
		}
		ts.Equal(fmt.Sprintf("this is file: %s", file.FileID), string(fileContent))
	}

	ts.mockBroker.AssertNumberOfCalls(ts.T(), "PublishMessage", 2)

	expectedJobMessage1, err := json.Marshal(&model.JobMessage{
		ValidationID:        validationInformation1.ValidationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: filepath.Join(ts.tempDir, validationInformation1.ValidationID),
		Files:               validationInformation1.Files,
	})
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	ts.mockBroker.AssertCalled(ts.T(), "PublishMessage", "job-queue", expectedJobMessage1)

	expectedJobMessage2, err := json.Marshal(&model.JobMessage{
		ValidationID:        validationInformation2.ValidationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: filepath.Join(ts.tempDir, validationInformation2.ValidationID),
		Files:               validationInformation2.Files,
	})
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	ts.mockBroker.AssertCalled(ts.T(), "PublishMessage", "job-queue", expectedJobMessage2)

	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-preparation-queue", "job-preparation-worker-0")
	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-preparation-queue", "job-preparation-worker-1")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 2)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "ReadValidationInformation", 2)
	ts.mockDatabase.AssertCalled(ts.T(), "ReadValidationInformation", validationInformation1.ValidationID)
	ts.mockDatabase.AssertCalled(ts.T(), "ReadValidationInformation", validationInformation2.ValidationID)
}

func (ts *JobPreparationWorkerTestSuite) TestWorkersConsumeDownloadError() {
	worker1MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-preparation-worker-0": worker1MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-preparation-queue", mock.Anything).Return(nil)

	httpTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "expected error")
	}))

	workers, err := NewWorkers(
		SourceQueue("job-preparation-queue"),
		DestinationQueue("job-queue"),
		SdaAPIURL(httpTestServer.URL),
		SdaAPIToken("mock-token"),
		Broker(ts.mockBroker),
		ValidationWorkDirectory(ts.tempDir),
		WorkerCount(1),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers.workers, 1)

	ts.mockBroker.On("PublishMessage", "job-queue", mock.Anything).Return(nil)

	for i, worker := range workers.workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-preparation-worker-%d", i), worker.id)
	}

	validationInformation1 := &model.ValidationInformation{
		ValidationID:     uuid.NewString(),
		ValidatorIDs:     []string{"mock-validator"},
		SubmissionUserID: "test_user",
		Files: []*model.FileInformation{
			{
				FileID:             "testFileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId2",
				FilePath:           "another_dir/file2",
				SubmissionFileSize: 1,
			}, {
				FileID:             "testFileId3",
				FilePath:           "file3",
				SubmissionFileSize: 1,
			},
		},
	}
	ts.mockDatabase.On("ReadValidationInformation", validationInformation1.ValidationID).Return(validationInformation1, nil)
	ts.mockDatabase.On("UpdateAllValidationJobFilesOnError", validationInformation1.ValidationID, mock.Anything).Return(nil)

	message1, err := json.Marshal(&model.JobPreparationMessage{ValidationID: validationInformation1.ValidationID})
	if err != nil {
		ts.FailNow("failed to marshal job preparation message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message1,
	}

	workers.Shutdown()

	for _, worker := range workers.workers {
		ts.Equal(false, worker.running)
	}

	ts.mockBroker.AssertNumberOfCalls(ts.T(), "PublishMessage", 0)
	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-preparation-queue", "job-preparation-worker-0")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 1)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "ReadValidationInformation", 1)
	ts.mockDatabase.AssertCalled(ts.T(), "ReadValidationInformation", validationInformation1.ValidationID)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateAllValidationJobFilesOnError", validationInformation1.ValidationID, mock.Anything)
}
