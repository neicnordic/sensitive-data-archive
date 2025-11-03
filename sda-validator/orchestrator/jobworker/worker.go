package jobworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/commandexecutor"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type worker struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc

	commandExecutor commandexecutor.CommandExecutor

	stopCh  chan struct{}
	error   error
	running bool
}

type Workers struct {
	workers           []*worker
	conf              *config
	workerMonitorChan chan error
}

// NewWorkers initializes the workers with the given options
func NewWorkers(opt ...func(*config)) (*Workers, error) {
	newWorkers := &Workers{
		conf: &config{},
	}

	for _, o := range opt {
		o(newWorkers.conf)
	}

	if newWorkers.conf.sourceQueue == "" {
		return nil, errors.New("sourceQueue is required")
	}

	if newWorkers.conf.broker == nil {
		return nil, errors.New("broker is required")
	}

	if newWorkers.conf.commandExecutor == nil {
		return nil, errors.New("commandExecutor is required")
	}

	newWorkers.workerMonitorChan = make(chan error, newWorkers.conf.workerCount)

	for i := 0; i < newWorkers.conf.workerCount; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		w := &worker{
			id:              fmt.Sprintf("job-worker-%d", i),
			ctx:             ctx,
			cancel:          cancel,
			stopCh:          make(chan struct{}, 1),
			running:         true,
			commandExecutor: newWorkers.conf.commandExecutor,
		}

		newWorkers.workers = append(newWorkers.workers, w)

		go func(w *worker) {
			// passing ctx such that we can gracefully shut down the subscribe
			if err := newWorkers.conf.broker.Subscribe(w.ctx, newWorkers.conf.sourceQueue, w.id, w.handleFunc); err != nil {
				log.Errorf("job worker encountered error: %v", err)
				newWorkers.workerMonitorChan <- err
			}
			w.running = false
			w.stopCh <- struct{}{}
		}(w)
	}

	return newWorkers, nil
}

// Monitor monitors if any worker encounters an subscribe error
func (w *Workers) Monitor() chan error {
	if w.conf == nil {
		noConfErr := make(chan error, 1)
		noConfErr <- errors.New("workers have not been initialized")

		return noConfErr
	}

	return w.workerMonitorChan
}

// close a worker and wait until it has closed
func (w *worker) close() {
	w.cancel()
	<-w.stopCh
	close(w.stopCh)
}

// Shutdown shutdowns and waits for all workers to have closed
func (w *Workers) Shutdown() {
	wg := sync.WaitGroup{}

	for _, w := range w.workers {
		if !w.running {
			continue
		}
		wg.Go(func() {
			w.close()
		})
	}

	wg.Wait()
	close(w.workerMonitorChan)
}

func (w *worker) handleFunc(ctx context.Context, message amqp.Delivery) (err error) {
	jobMessage := new(model.JobMessage)
	if err := json.Unmarshal(message.Body, jobMessage); err != nil {
		log.Errorf("could not unmarshal message to job message due to: %v", err)

		return nil // returning nil so message is not nacked and reconsumed
	}

	jobDirectory := filepath.Join(jobMessage.ValidationDirectory, jobMessage.ValidatorID)
	// Remove job directory if any error is encountered
	defer func() {
		if err == nil {
			return
		}
		if err := os.RemoveAll(jobDirectory); err != nil {
			log.Errorf("failed to remove job directory after worker encountered an error due to: %v", err)
		}
	}()

	if err := os.MkdirAll(filepath.Join(jobDirectory, "output"), 0750); err != nil {
		log.Errorf("failed to create job output directory at: %s, error: %v", jobDirectory, err)

		return err
	}

	if err := os.MkdirAll(filepath.Join(jobDirectory, "input"), 0750); err != nil {
		log.Errorf("failed to create job input directory at: %s, error: %v", jobDirectory, err)

		return err
	}

	input := &model.ValidatorInput{
		Files:  nil,
		Paths:  nil,
		Config: nil,
	}

	validatorDescription, ok := validators.Validators[jobMessage.ValidatorID]
	if !ok {
		return fmt.Errorf("validator %s no longer found as a valid validator", jobMessage.ValidatorID)
	}

	for _, fileInfo := range jobMessage.Files {
		filePathForJob := filepath.Join("/mnt/input/data", fileInfo.FilePath)
		switch validatorDescription.Mode {
		case "file", "file-pair":
			input.Files = append(input.Files, &model.FileInput{Path: filePathForJob})
		case "file-structure":
			input.Paths = append(input.Paths, filePathForJob)
		default:
			return updateFileValidationJobsOnError(ctx, jobMessage, []*model.Message{{Level: "error", Message: fmt.Sprintf("validator has unknown mode: %s", validatorDescription.Mode), Time: time.Now().Format(time.RFC3339)}})
		}
	}

	inputData, err := json.Marshal(input)
	if err != nil {
		log.Errorf("failed to marshal input data due to: %v", err)

		return err
	}
	if err := os.WriteFile(filepath.Join(jobDirectory, "input", "input.json"), inputData, 0600); err != nil {
		log.Errorf("failed to write input.json for validator due to: %v", err)

		return err
	}

	// Here we mount the validatorJobDir as /mnt with the input, and output directories such that validator can access input/input.json and write a output/result.json
	// we also mount validatorJobDir/files/ as /mnt/input/data such that the validator can access the files without the need for us to duplicate them per validator
	_, err = w.commandExecutor.Execute(
		"apptainer",
		"run",
		"--userns",
		"--net",
		"--network", "none",
		"--bind", fmt.Sprintf("%s:/mnt", jobDirectory),
		"--bind", fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobMessage.ValidationDirectory, "files")),
		validatorDescription.ValidatorPath)

	if err != nil {
		log.Errorf("failed to execute run command due to: %s", err)

		return updateFileValidationJobsOnError(ctx, jobMessage, []*model.Message{{Level: "error", Message: fmt.Sprintf("failed to execute run command due to: %s", err), Time: time.Now().Format(time.RFC3339)}})
	}

	result, err := os.ReadFile(filepath.Join(jobDirectory, "/output/result.json"))
	if err != nil {
		log.Errorf("failed to read result file: %v", err)

		return updateFileValidationJobsOnError(ctx, jobMessage, []*model.Message{{Level: "error", Message: fmt.Sprintf("failed to read result file: %v", err), Time: time.Now().Format(time.RFC3339)}})
	}

	validatorOutput := new(model.ValidatorOutput)
	if err := json.Unmarshal(result, validatorOutput); err != nil {
		log.Errorf("failed to unmarshal result file: %v", err)

		return updateFileValidationJobsOnError(ctx, jobMessage, []*model.Message{{Level: "error", Message: fmt.Sprintf("failed to unmarshal result file: %v", err), Time: time.Now().Format(time.RFC3339)}})
	}

	return updateFileValidationJobs(ctx, jobMessage, validatorOutput)
}
func updateFileValidationJobs(ctx context.Context, jobMessage *model.JobMessage, validatorOutput *model.ValidatorOutput) error {
	tx, err := database.BeginTransaction(ctx)
	if err != nil {
		log.Errorf("failed to begin transaction: %v", err)

		return err
	}

	now := time.Now()
	for _, fileInfo := range jobMessage.Files {
		var fileResult *model.FileResult
		for _, fr := range validatorOutput.Files {
			filePath, _ := strings.CutPrefix(fr.FilePath, "/mnt/input/data/")
			if filePath == fileInfo.FilePath {
				fileResult = fr

				break
			}
		}
		if fileResult == nil {
			if err := tx.UpdateFileValidationJob(ctx, &model.UpdateFileValidationJobParameters{
				ValidationID:      jobMessage.ValidationID,
				ValidatorID:       jobMessage.ValidatorID,
				FileID:            fileInfo.FileID,
				FileResult:        "error",
				ValidatorResult:   validatorOutput.Result,
				FileMessages:      []*model.Message{{Level: "error", Message: "file result not found in validator output", Time: time.Now().Format(time.RFC3339)}},
				FinishedAt:        now,
				ValidatorMessages: validatorOutput.Messages,
			}); err != nil {
				log.Errorf("failed to update file validation job on file missing from result file due to: %v", err)

				return err
			}

			continue
		}

		if err := tx.UpdateFileValidationJob(ctx, &model.UpdateFileValidationJobParameters{
			ValidationID:      jobMessage.ValidationID,
			ValidatorID:       jobMessage.ValidatorID,
			FileID:            fileInfo.FileID,
			FileResult:        fileResult.Result,
			ValidatorResult:   validatorOutput.Result,
			FileMessages:      fileResult.Messages,
			FinishedAt:        now,
			ValidatorMessages: validatorOutput.Messages,
		}); err != nil {
			log.Errorf("failed to update file validation job due to: %v", err)

			return err
		}
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit transaction due to: %v", err)

		return err
	}

	return checkAndCleanVolume(ctx, jobMessage.ValidationID, jobMessage.ValidationDirectory)
}

func checkAndCleanVolume(ctx context.Context, validationID, validationDirectory string) error {
	allJobsDone, err := database.AllValidationJobsDone(ctx, validationID)
	if err != nil {
		log.Errorf("failed to check if all validation jobs done due to: %v", err)

		return err
	}

	if !allJobsDone {
		return nil
	}

	if err := os.RemoveAll(validationDirectory); err != nil {
		log.Errorf("failed to remove validation directory when all jobs done, due to %v", err)
	}

	return nil
}

func updateFileValidationJobsOnError(ctx context.Context, jobMessage *model.JobMessage, validatorMessages []*model.Message) error {
	tx, err := database.BeginTransaction(ctx)
	if err != nil {
		log.Errorf("failed to begin transaction: %v", err)

		return err
	}
	// Deferring rollback as if tx has been commited rollback wont be actioned
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Errorf("failed to rollback transaction due to: %v", err)
		}
	}()

	now := time.Now()

	for _, fileInfo := range jobMessage.Files {
		if err := tx.UpdateFileValidationJob(ctx, &model.UpdateFileValidationJobParameters{
			ValidationID:      jobMessage.ValidationID,
			ValidatorID:       jobMessage.ValidatorID,
			FileID:            fileInfo.FileID,
			FileResult:        "error",
			ValidatorResult:   "error",
			FileMessages:      nil,
			FinishedAt:        now,
			ValidatorMessages: validatorMessages,
		}); err != nil {
			log.Errorf("failed to update file validation job due to: %v", err)

			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return checkAndCleanVolume(ctx, jobMessage.ValidationID, jobMessage.ValidationDirectory)
}
