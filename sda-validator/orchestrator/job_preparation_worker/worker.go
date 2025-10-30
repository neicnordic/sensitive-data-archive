package job_preparation_worker

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

	stopCh  chan struct{}
	running bool
}

var workers []*worker
var conf *config
var shutdownChan chan struct{}

// Init initializes the workers with the given options
func Init(opt ...func(*config)) error {
	workers = []*worker{}
	shutdownChan = make(chan struct{}, 1)

	conf = &config{}

	for _, o := range opt {
		o(conf)
	}

	if conf.sourceQueue == "" {
		return errors.New("sourceQueue is required")
	}

	if conf.destinationQueue == "" {
		return errors.New("destinationQueue is required")
	}
	if conf.sdaApiUrl == "" {
		return errors.New("sdaApiUrl is required")
	}
	if conf.sdaApiToken == "" {
		return errors.New("sdaApiToken is required")
	}
	if conf.broker == nil {
		return errors.New("broker is required")
	}

	if conf.validationWorkDir == "" {
		return errors.New("validationWorkDir is required")
	}

	for i := 0; i < conf.workerCount; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		w := &worker{
			id:      fmt.Sprintf("job-preparation-worker-%d", i),
			ctx:     ctx,
			cancel:  cancel,
			stopCh:  make(chan struct{}, 1),
			running: false,
		}

		workers = append(workers, w)
	}

	return nil
}

// StartWorkers starts all initialized workers and waits for the workers to either encounter an error or for shutdown to be triggered
func StartWorkers() error {
	if conf == nil {
		return errors.New("workers have not been initialized")
	}

	errChan := make(chan error, 1)

	wg := &sync.WaitGroup{}
	for _, w := range workers {
		wg.Add(1)
		go func(w *worker) {
			wg.Done()
			w.running = true
			// passing ctx such that we can gracefully shut down the subscribe
			if err := conf.broker.Subscribe(w.ctx, conf.sourceQueue, w.id, w.handleFunc); err != nil {
				errChan <- errors.Join(errors.New("job preparation worker encountered error"), err)
			}

			w.stopCh <- struct{}{}
			w.running = false
		}(w)
	}

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	case <-shutdownChan:
		close(shutdownChan)
		return nil
	}
}

// close a worker and wait until it has closed
func (w *worker) close() {
	w.cancel()
	<-w.stopCh
	close(w.stopCh)
}

// ShutdownWorkers shutdowns and waits for all workers to have closed
func ShutdownWorkers() {
	wg := sync.WaitGroup{}
	for _, w := range workers {
		if !w.running {
			continue
		}
		wg.Add(1)
		go func() {
			w.close()
			wg.Done()
		}()
	}
	wg.Wait()
	shutdownChan <- struct{}{}
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

	validationDir := filepath.Join(conf.validationWorkDir, jobPreparationMessage.ValidationID)
	validationFilesDir := filepath.Join(validationDir, "files")
	if err = os.MkdirAll(validationFilesDir, 0755); err != nil {
		log.Errorf("failed to create validation work directory: %s, error: %v", validationFilesDir, err)
		return err
	}
	// Remove validation directory if any error is encountered
	defer func() {
		if err == nil {
			return
		}
		if err := os.RemoveAll(filepath.Join(conf.validationWorkDir, jobPreparationMessage.ValidationID)); err != nil {
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
		if err := downloadFiles(ctx, validationFilesDir, validationInformation); err != nil {
			log.Errorf("failed to download files, error: %v", err)
			return err
		}
	}

	return w.sendValidatorJobs(ctx, validationDir, validationInformation)
}

func downloadFiles(ctx context.Context, validationFilesDir string, validationInformation *model.ValidationInformation) error {

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
		if err := os.MkdirAll(dir, 0755); err != nil {
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
	for fileId, file := range files {
		if err := downloadFile(ctx, validationInformation.SubmissionUserID, fileId, file, pubKeyBase64, privateKeyData); err != nil {
			return err
		}
	}

	return nil
}

func downloadFile(_ context.Context, userID, fileID string, file *os.File, pubKeyBase64 string, privateKeyData [32]byte) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/file/%s", conf.sdaApiUrl, userID, fileID), nil)
	if err != nil {
		return fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// TODO how to handle auth in better way, TBD #989
	req.Header.Add("Authorization", "Bearer "+conf.sdaApiToken)
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
		for i, fileInfo := range validationInformation.Files {
			jobMessage.Files[i] = fileInfo
		}

		body, err := json.Marshal(jobMessage)
		if err != nil {
			return fmt.Errorf("failed to marshal job message due to: %s", err)
		}

		if err := conf.broker.PublishMessage(ctx, conf.destinationQueue, body); err != nil {
			return fmt.Errorf("failed to publish message due to: %s", err)
		}
	}
	return nil
}
