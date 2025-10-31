package jobworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type JobWorkerTestSuite struct {
	suite.Suite

	tempDir string

	mockDatabase        *mockDatabase
	mockBroker          *mockBroker
	mockCommandExecutor *mockCommandExecutor
}

func (ts *JobWorkerTestSuite) SetupSuite() {
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
func (ts *JobWorkerTestSuite) SetupTest() {
	ts.tempDir = ts.T().TempDir()
	// Reset any Asserts and On() on mocks from previous tests
	ts.mockDatabase = &mockDatabase{}
	ts.mockBroker = &mockBroker{}
	ts.mockCommandExecutor = &mockCommandExecutor{}
	database.RegisterDatabase(ts.mockDatabase)
}

func (ts *JobWorkerTestSuite) TearDownTest() {
}

func (ts *JobWorkerTestSuite) TearDownSuite() {
}

func TestJobPreparationWorkerTestSuite(t *testing.T) {
	suite.Run(t, new(JobWorkerTestSuite))
}

type mockCommandExecutor struct {
	mock.Mock
}

func (m *mockCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	mockArgs := m.Called(name, args)
	mockArgs.Get(0).(func())()

	return nil, mockArgs.Error(1)
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
	// Function not needed for unit test, but to implement interface
	panic("database.Close call not expected in unit tests")
}

func (m *mockDatabase) ReadValidationResult(_ context.Context, _ string, _ *string) (*model.ValidationResult, error) {
	// Function not needed for unit test, but to implement interface
	panic("database.ReadValidationResult call not expected in unit tests")
}

func (m *mockDatabase) ReadValidationInformation(_ context.Context, _ string) (*model.ValidationInformation, error) {
	// Function not needed for unit test, but to implement interface
	panic("database.ReadValidationInformation call not expected in unit tests")
}

func (m *mockDatabase) InsertFileValidationJob(_ context.Context, _ *model.InsertFileValidationJobParameters) error {
	// Function not needed for unit test, but to implement interface
	panic("database.InsertFileValidationJob call not expected in unit tests")
}

func (m *mockDatabase) UpdateFileValidationJob(_ context.Context, params *model.UpdateFileValidationJobParameters) error {
	args := m.Called(params.ValidationID, params.ValidatorID, params.FileID, params.FileResult, params.FileMessages, params.FinishedAt, params.ValidatorResult, params.ValidatorMessages)

	return args.Error(0)
}

func (m *mockDatabase) AllValidationJobsDone(_ context.Context, validationID string) (bool, error) {
	args := m.Called(validationID)

	return args.Bool(0), args.Error(1)
}

type mockBroker struct {
	mock.Mock
	messageChans map[string]chan amqp.Delivery
}

func (m *mockBroker) PublishMessage(_ context.Context, _ string, _ []byte) error {
	// Function not needed for unit test, but to implement interface
	panic("broker.PublishMessage call not expected in unit tests")
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

func (m *mockBroker) ConnectionWatcher() chan *amqp.Error {
	// Function not needed for unit test, but to implement interface
	panic("broker.ConnectionWatcher call not expected in unit tests")
}

func (ts *JobWorkerTestSuite) TestInitWorkers() {
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)
	ts.NoError(Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(2),
	))
	ts.Len(workers, 2)
	ShutdownWorkers()
}

func (ts *JobWorkerTestSuite) TestInitWorkers_NoSourceQueue() {
	ts.EqualError(Init(
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(2),
	), "sourceQueue is required")
}

func (ts *JobWorkerTestSuite) TestInitWorkers_NoBroker() {
	ts.EqualError(Init(
		SourceQueue("job-queue"),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(2),
	), "broker is required")
}

func (ts *JobWorkerTestSuite) TestInitWorkers_NoCommandExecutor() {
	ts.EqualError(Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		WorkerCount(2),
	), "commandExecutor is required")
}

func (ts *JobWorkerTestSuite) TestStartWorkers_NoInit() {
	conf = nil
	select {
	case <-time.After(2 * time.Second):
		ts.FailNow("timeout error, expected MonitorWorker to return error")
	case err := <-MonitorWorkers():
		ts.EqualError(err, "workers have not been initialized")
	}
}

func (ts *JobWorkerTestSuite) TestStartWorkers_SubscribeError() {
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(errors.New("subscribe error"))

	if err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(2),
	); err != nil {
		ts.FailNow(err.Error())
	}

	select {
	case <-time.After(2 * time.Second):
		ts.FailNow("timeout error, expected MonitorWorker to return error")
	case err := <-MonitorWorkers():
		ts.EqualError(err, "subscribe error")
	}
	ShutdownWorkers()
}

func (ts *JobWorkerTestSuite) TestStartAndShutdownWorkers() {
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-worker-0": make(chan amqp.Delivery),
		"job-worker-1": make(chan amqp.Delivery),
	}
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)

	if err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(2),
	); err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers, 2)

	for i, worker := range workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-worker-%d", i), worker.id)
	}

	ShutdownWorkers()

	for _, worker := range workers {
		ts.Equal(false, worker.running)
	}
}

func (ts *JobWorkerTestSuite) TestWorkersConsume() {
	worker1MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-worker-0": worker1MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)

	err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(1),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers, 1)

	for i, worker := range workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-worker-%d", i), worker.id)
	}

	validationID := uuid.NewString()
	validationDir := filepath.Join(ts.tempDir, validationID)

	if err := os.MkdirAll(filepath.Join(validationDir, "files"), 0750); err != nil {
		ts.FailNow("failed to create validation dir", err)
	}
	jobMessage := &model.JobMessage{
		ValidationID:        validationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: validationDir,
		Files: []*model.FileInformation{
			{
				FileID:             "fileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId2",
				FilePath:           "another_dir/file2",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId3",
				FilePath:           "file3",
				SubmissionFileSize: 1,
			},
		},
	}

	for _, file := range jobMessage.Files {
		filePath := filepath.Join(validationDir, "files", file.FilePath)
		fileDir := filepath.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0750); err != nil {
			ts.FailNow("failed to create validation file dir", err)
		}
		if err := os.WriteFile(filePath, []byte(fmt.Sprintf("This is file: %s", file.FileID)), 0400); err != nil {
			ts.FailNow("failed to create validation file", err)
		}
	}

	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("UpdateFileValidationJob", validationID, "mock-validator", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ts.mockDatabase.On("AllValidationJobsDone", validationID).Return(true, nil)

	expectedResult := &model.ValidatorOutput{
		Result: "failed",
		Files: []*model.FileResult{
			{
				FilePath: "/mnt/input/data/test_dir/file1",
				Result:   "failed",
				Messages: []*model.Message{{
					Level:   "INFO",
					Time:    time.Now().Format(time.RFC3339),
					Message: "File failed validation",
				}},
			}, {
				FilePath: "/mnt/input/data/another_dir/file2",
				Result:   "passed",
				Messages: nil,
			}, {
				FilePath: "/mnt/input/data/file3",
				Result:   "passed",
				Messages: nil,
			},
		},
		Messages: nil,
	}
	resultJSON, err := json.Marshal(expectedResult)
	if err != nil {
		ts.FailNow("failed to marshal expected result", err)
	}

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"--bind", fmt.Sprintf("%s:/mnt", filepath.Join(validationDir, "mock-validator")),
			"--bind", fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobMessage.ValidationDirectory, "files")),
			"/mock-validator.sif"}).Return(func() {
		if err := os.WriteFile(filepath.Join(validationDir, "mock-validator", "output", "result.json"), resultJSON, 0400); err != nil {
			ts.Fail("failed to create result file")
		}
	}, nil)

	message, err := json.Marshal(jobMessage)
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message,
	}

	ShutdownWorkers()

	for _, worker := range workers {
		ts.Equal(false, worker.running)
	}

	// Check validation dir was deleted
	_, err = os.ReadDir(validationDir)
	ts.EqualError(err, fmt.Sprintf("open %s: no such file or directory", validationDir))

	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-queue", "job-worker-0")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 1)

	ts.mockDatabase.AssertCalled(ts.T(), "AllValidationJobsDone", validationID)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "AllValidationJobsDone", 1)

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "UpdateFileValidationJob", 3)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId1", expectedResult.Files[0].Result, expectedResult.Files[0].Messages, mock.Anything, expectedResult.Result, expectedResult.Messages)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId2", expectedResult.Files[1].Result, expectedResult.Files[1].Messages, mock.Anything, expectedResult.Result, expectedResult.Messages)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId3", expectedResult.Files[2].Result, expectedResult.Files[2].Messages, mock.Anything, expectedResult.Result, expectedResult.Messages)
}

func (ts *JobWorkerTestSuite) TestWorkersConsume_ErrorOnApptainerRun() {
	worker1MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-worker-0": worker1MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)

	err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(1),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers, 1)

	for i, worker := range workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-worker-%d", i), worker.id)
	}

	validationID := uuid.NewString()
	validationDir := filepath.Join(ts.tempDir, validationID)

	if err := os.MkdirAll(filepath.Join(validationDir, "files"), 0750); err != nil {
		ts.FailNow("failed to create validation dir", err)
	}
	jobMessage := &model.JobMessage{
		ValidationID:        validationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: validationDir,
		Files: []*model.FileInformation{
			{
				FileID:             "fileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			},
		},
	}

	for _, file := range jobMessage.Files {
		filePath := filepath.Join(validationDir, "files", file.FilePath)
		fileDir := filepath.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0750); err != nil {
			ts.FailNow("failed to create validation file dir", err)
		}
		if err := os.WriteFile(filePath, []byte(fmt.Sprintf("This is file: %s", file.FileID)), 0400); err != nil {
			ts.FailNow("failed to create validation file", err)
		}
	}

	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("UpdateFileValidationJob", validationID, "mock-validator", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ts.mockDatabase.On("AllValidationJobsDone", validationID).Return(true, nil)

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"--bind", fmt.Sprintf("%s:/mnt", filepath.Join(validationDir, "mock-validator")),
			"--bind", fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobMessage.ValidationDirectory, "files")),
			"/mock-validator.sif"}).Return(func() {}, errors.New("expected error from apptainer"))

	message, err := json.Marshal(jobMessage)
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message,
	}

	ShutdownWorkers()

	for _, worker := range workers {
		ts.Equal(false, worker.running)
	}

	// Check validation dir was deleted
	_, err = os.ReadDir(validationDir)
	ts.EqualError(err, fmt.Sprintf("open %s: no such file or directory", validationDir))

	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-queue", "job-worker-0")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 1)

	ts.mockDatabase.AssertCalled(ts.T(), "AllValidationJobsDone", validationID)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "AllValidationJobsDone", 1)

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "UpdateFileValidationJob", 1)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId1", "error", mock.Anything, mock.Anything, "error", mock.Anything)
}

func (ts *JobWorkerTestSuite) TestWorkersConsume_NoResultFileFromApptainer() {
	worker1MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-worker-0": worker1MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)

	err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(1),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers, 1)

	for i, worker := range workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-worker-%d", i), worker.id)
	}

	validationID := uuid.NewString()
	validationDir := filepath.Join(ts.tempDir, validationID)

	if err := os.MkdirAll(filepath.Join(validationDir, "files"), 0750); err != nil {
		ts.FailNow("failed to create validation dir", err)
	}
	jobMessage := &model.JobMessage{
		ValidationID:        validationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: validationDir,
		Files: []*model.FileInformation{
			{
				FileID:             "fileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId2",
				FilePath:           "another_dir/file2",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId3",
				FilePath:           "file3",
				SubmissionFileSize: 1,
			},
		},
	}

	for _, file := range jobMessage.Files {
		filePath := filepath.Join(validationDir, "files", file.FilePath)
		fileDir := filepath.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0750); err != nil {
			ts.FailNow("failed to create validation file dir", err)
		}
		if err := os.WriteFile(filePath, []byte(fmt.Sprintf("This is file: %s", file.FileID)), 0400); err != nil {
			ts.FailNow("failed to create validation file", err)
		}
	}

	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("UpdateFileValidationJob", validationID, "mock-validator", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ts.mockDatabase.On("AllValidationJobsDone", validationID).Return(true, nil)

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"--bind", fmt.Sprintf("%s:/mnt", filepath.Join(validationDir, "mock-validator")),
			"--bind", fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobMessage.ValidationDirectory, "files")),
			"/mock-validator.sif"}).Return(func() {}, nil)

	message, err := json.Marshal(jobMessage)
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message,
	}

	ShutdownWorkers()

	for _, worker := range workers {
		ts.Equal(false, worker.running)
	}

	// Check validation dir was deleted
	_, err = os.ReadDir(validationDir)
	ts.EqualError(err, fmt.Sprintf("open %s: no such file or directory", validationDir))

	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-queue", "job-worker-0")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 1)

	ts.mockDatabase.AssertCalled(ts.T(), "AllValidationJobsDone", validationID)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "AllValidationJobsDone", 1)

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "UpdateFileValidationJob", 3)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId1", "error", mock.Anything, mock.Anything, "error", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId2", "error", mock.Anything, mock.Anything, "error", mock.Anything)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId3", "error", mock.Anything, mock.Anything, "error", mock.Anything)
}

func (ts *JobWorkerTestSuite) TestWorkersConsume_MissingFileInResultFile() {
	worker1MessageChan := make(chan amqp.Delivery)
	ts.mockBroker.messageChans = map[string]chan amqp.Delivery{
		"job-worker-0": worker1MessageChan,
	}
	ts.mockBroker.On("Subscribe", "job-queue", mock.Anything).Return(nil)

	err := Init(
		SourceQueue("job-queue"),
		Broker(ts.mockBroker),
		CommandExecutor(ts.mockCommandExecutor),
		WorkerCount(1),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}
	ts.Len(workers, 1)

	for i, worker := range workers {
		ts.Equal(true, worker.running)
		ts.Equal(fmt.Sprintf("job-worker-%d", i), worker.id)
	}

	validationID := uuid.NewString()
	validationDir := filepath.Join(ts.tempDir, validationID)

	if err := os.MkdirAll(filepath.Join(validationDir, "files"), 0750); err != nil {
		ts.FailNow("failed to create validation dir", err)
	}
	jobMessage := &model.JobMessage{
		ValidationID:        validationID,
		ValidatorID:         "mock-validator",
		ValidationDirectory: validationDir,
		Files: []*model.FileInformation{
			{
				FileID:             "fileId1",
				FilePath:           "test_dir/file1",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId2",
				FilePath:           "another_dir/file2",
				SubmissionFileSize: 1,
			}, {
				FileID:             "fileId3",
				FilePath:           "file3",
				SubmissionFileSize: 1,
			},
		},
	}

	for _, file := range jobMessage.Files {
		filePath := filepath.Join(validationDir, "files", file.FilePath)
		fileDir := filepath.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0750); err != nil {
			ts.FailNow("failed to create validation file dir", err)
		}
		if err := os.WriteFile(filePath, []byte(fmt.Sprintf("This is file: %s", file.FileID)), 0400); err != nil {
			ts.FailNow("failed to create validation file", err)
		}
	}

	ts.mockDatabase.On("Rollback").Return(nil)
	ts.mockDatabase.On("BeginTransaction").Return(nil)
	ts.mockDatabase.On("Commit").Return(nil)
	ts.mockDatabase.On("UpdateFileValidationJob", validationID, "mock-validator", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ts.mockDatabase.On("AllValidationJobsDone", validationID).Return(true, nil)

	expectedResult := &model.ValidatorOutput{
		Result: "passed",
		Files: []*model.FileResult{
			{
				FilePath: "/mnt/input/data/another_dir/file2",
				Result:   "passed",
				Messages: nil,
			}, {
				FilePath: "/mnt/input/data/file3",
				Result:   "passed",
				Messages: nil,
			},
		},
		Messages: nil,
	}
	resultJSON, err := json.Marshal(expectedResult)
	if err != nil {
		ts.FailNow("failed to marshal expected result", err)
	}

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"--bind", fmt.Sprintf("%s:/mnt", filepath.Join(validationDir, "mock-validator")),
			"--bind", fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobMessage.ValidationDirectory, "files")),
			"/mock-validator.sif"}).Return(func() {
		if err := os.WriteFile(filepath.Join(validationDir, "mock-validator", "output", "result.json"), resultJSON, 0400); err != nil {
			ts.Fail("failed to create result file")
		}
	}, nil)

	message, err := json.Marshal(jobMessage)
	if err != nil {
		ts.FailNow("failed to marshal job message", err)
	}
	worker1MessageChan <- amqp.Delivery{
		Body: message,
	}

	ShutdownWorkers()

	for _, worker := range workers {
		ts.Equal(false, worker.running)
	}

	// Check validation dir was deleted
	_, err = os.ReadDir(validationDir)
	ts.EqualError(err, fmt.Sprintf("open %s: no such file or directory", validationDir))

	ts.mockBroker.AssertCalled(ts.T(), "Subscribe", "job-queue", "job-worker-0")
	ts.mockBroker.AssertNumberOfCalls(ts.T(), "Subscribe", 1)

	ts.mockDatabase.AssertCalled(ts.T(), "AllValidationJobsDone", validationID)
	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "AllValidationJobsDone", 1)

	ts.mockDatabase.AssertNumberOfCalls(ts.T(), "UpdateFileValidationJob", 3)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId1", "error", mock.Anything, mock.Anything, expectedResult.Result, expectedResult.Messages)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId2", expectedResult.Files[0].Result, expectedResult.Files[0].Messages, mock.Anything, expectedResult.Result, expectedResult.Messages)
	ts.mockDatabase.AssertCalled(ts.T(), "UpdateFileValidationJob", validationID, "mock-validator", "fileId3", expectedResult.Files[1].Result, expectedResult.Files[1].Messages, mock.Anything, expectedResult.Result, expectedResult.Messages)
}
