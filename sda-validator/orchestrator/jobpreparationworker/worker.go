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
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	amqp "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

type worker struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc

	conf *config

	stopCh  chan struct{}
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

	if newWorkers.conf.destinationQueue == "" {
		return nil, errors.New("destinationQueue is required")
	}
	if newWorkers.conf.sdaAPIURL == "" {
		return nil, errors.New("sdaAPIURL is required")
	}
	if newWorkers.conf.sdaAPIToken == "" {
		return nil, errors.New("sdaAPIToken is required")
	}
	if newWorkers.conf.broker == nil {
		return nil, errors.New("broker is required")
	}

	if newWorkers.conf.validationWorkDir == "" {
		return nil, errors.New("validationWorkDir is required")
	}

	newWorkers.workerMonitorChan = make(chan error, newWorkers.conf.workerCount)

	for i := 0; i < newWorkers.conf.workerCount; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		w := &worker{
			id:      fmt.Sprintf("job-preparation-worker-%d", i),
			ctx:     ctx,
			cancel:  cancel,
			stopCh:  make(chan struct{}, 1),
			conf:    newWorkers.conf,
			running: true,
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
	jobPreparationMessage := new(model.JobPreparationMessage)
	if err := json.Unmarshal(message.Body, jobPreparationMessage); err != nil {
		log.Errorf("could not unmarshal message to job preparation message due to: %v", err)

		return nil // returning nil so message is not nacked and reconsumed
	}

	validationInformation, err := database.ReadValidationInformation(ctx, jobPreparationMessage.ValidationID)
	if err != nil {
		log.Warnf("could not read validation information due to: %v", err)

		return err
	}

	if validationInformation == nil {
		log.Warnf("received job preparation message with validation id: %s, which had no file validation jobs in database", jobPreparationMessage.ValidationID)

		return nil
	}

	validationDir := filepath.Join(w.conf.validationWorkDir, jobPreparationMessage.ValidationID)
	validationFilesDir := filepath.Join(validationDir, "files")
	if err = os.MkdirAll(validationFilesDir, 0750); err != nil {
		log.Errorf("failed to create validation work directory: %s, error: %v", validationFilesDir, err)

		return database.UpdateAllValidationJobFilesOnError(ctx, jobPreparationMessage.ValidationID, &model.Message{
			Level:   "error",
			Time:    time.Now().Format(time.RFC3339),
			Message: "Internal error",
		})
	}
	// Remove validation directory if any error is encountered
	defer func() {
		if err == nil {
			return
		}
		if err := os.RemoveAll(filepath.Join(w.conf.validationWorkDir, jobPreparationMessage.ValidationID)); err != nil {
			log.Errorf("failed to remove validation directory after worker encountered an error due to: %v", err)
		}
	}()

	requiresFileContent := false
	for _, validatorID := range validationInformation.ValidatorIDs {
		if validatorDescription, ok := validators.Validators[validatorID]; ok && validatorDescription.RequiresFileContent() {
			requiresFileContent = true

			break
		}
	}

	if requiresFileContent {
		if err := w.downloadFiles(ctx, validationFilesDir, validationInformation); err != nil {
			log.Errorf("failed to download files, error: %v", err)

			if err := os.RemoveAll(filepath.Join(w.conf.validationWorkDir, jobPreparationMessage.ValidationID)); err != nil {
				log.Errorf("failed to remove validation directory after worker encountered an error due to: %v", err)
			}
			return database.UpdateAllValidationJobFilesOnError(ctx, jobPreparationMessage.ValidationID, &model.Message{
				Level:   "error",
				Time:    time.Now().Format(time.RFC3339),
				Message: "Internal error",
			})
		}
	}

	return w.sendValidatorJobs(ctx, validationDir, validationInformation)
}

func (w *worker) downloadFiles(ctx context.Context, validationFilesDir string, validationInformation *model.ValidationInformation) error {
	files := make(map[string]*os.File)
	// Ensure all files are closed
	defer func(filesToClose map[string]*os.File) {
		for _, file := range files {
			_ = file.Close()
		}
	}(files)

	// Reserve disk space for all files to be downloaded
	for _, fileInformation := range validationInformation.Files {
		fileLocalPath := filepath.Join(validationFilesDir, fileInformation.FilePath)
		dir, _ := filepath.Split(fileLocalPath)

		// Ensure any sub directories file is in is created
		if err := os.MkdirAll(dir, 0750); err != nil {
			log.Errorf("failed to create sub directory for file: %s, error: %v", dir, err)

			return err
		}

		file, err := os.OpenFile(fileLocalPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0444)
		if err != nil {
			log.Errorf("failed to open file: %s, error: %v", fileLocalPath, err)

			return err
		}

		if err := file.Truncate(fileInformation.SubmissionFileSize); err != nil {
			log.Errorf("failed to truncate file: %s, error: %v", fileLocalPath, err)

			return err
		}
		files[fileInformation.FileID] = file
	}

	publicKeyData, privateKeyData, err := keys.GenerateKeyPair()
	if err != nil {
		return err
	}
	buf := new(bytes.Buffer)
	if err := keys.WriteCrypt4GHX25519PublicKey(buf, publicKeyData); err != nil {
		return err
	}
	pubKeyBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Download and mount files to local
	for fileID, file := range files {
		if err := w.downloadFile(ctx, validationInformation.SubmissionUserID, fileID, file, pubKeyBase64, privateKeyData); err != nil {
			return err
		}
	}

	return nil
}

func (w *worker) downloadFile(_ context.Context, userID, fileID string, file *os.File, pubKeyBase64 string, privateKeyData [32]byte) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/file/%s", w.conf.sdaAPIURL, userID, fileID), nil)
	if err != nil {
		return fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// TODO how to handle auth in better way, TBD #989
	req.Header.Add("Authorization", "Bearer "+w.conf.sdaAPIToken)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("C4GH-Public-Key", pubKeyBase64)

	// Send the request
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get response, reason: %v", err)
	}
	// Check the status code
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: for: %s", res.StatusCode, req.URL.String())
	}
	defer func() {
		_ = res.Body.Close()
	}()

	// Decrypt file
	crypt4GHReader, err := streaming.NewCrypt4GHReader(res.Body, privateKeyData, nil)
	if err != nil {
		return fmt.Errorf("could not create cryp4gh reader: %v", err)
	}
	defer func() {
		_ = crypt4GHReader.Close()
	}()

	_, err = io.Copy(file, crypt4GHReader)
	if err != nil {
		return fmt.Errorf("could not decrypt fileID %s, userID: %s, error: %v", fileID, userID, err)
	}

	return nil
}

func (w *worker) sendValidatorJobs(ctx context.Context, validationDir string, validationInformation *model.ValidationInformation) error {
	files := make([]string, len(validationInformation.Files))

	for i, file := range validationInformation.Files {
		files[i] = file.FilePath
	}

	for _, validatorID := range validationInformation.ValidatorIDs {
		jobMessage := model.JobMessage{
			ValidationID:        validationInformation.ValidationID,
			ValidatorID:         validatorID,
			ValidationDirectory: validationDir,
			Files:               make([]*model.FileInformation, len(validationInformation.Files)),
		}
		copy(jobMessage.Files, validationInformation.Files)

		body, err := json.Marshal(jobMessage)
		if err != nil {
			return fmt.Errorf("failed to marshal job message due to: %s", err)
		}

		if err := w.conf.broker.PublishMessage(ctx, w.conf.destinationQueue, body); err != nil {
			return fmt.Errorf("failed to publish message due to: %s", err)
		}
	}

	return nil
}
