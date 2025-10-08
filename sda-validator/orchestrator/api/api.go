package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	commandExecutor "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/command_executor.go"
	validatorAPI "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/openapi/go-gin-server/go"
	log "github.com/sirupsen/logrus"
)

type validatorAPIImpl struct {
	validatorPaths    []string
	sdaApiUrl         string
	sdaApiToken       string
	commandExecutor   commandExecutor.CommandExecutor
	validationWorkDir string
}

func NewValidatorAPIImpl(options ...func(*validatorAPIImpl)) (validatorAPI.ValidatorAPI, error) {

	impl := &validatorAPIImpl{
		commandExecutor: commandExecutor.OsCommandExecutor{},
	}

	for _, option := range options {
		option(impl)
	}

	if impl.sdaApiUrl == "" {
		return nil, errors.New("sdaApiUrl is required")
	}
	if impl.sdaApiToken == "" {
		return nil, errors.New("sdaApiToken is required")
	}
	if impl.validationWorkDir == "" {
		return nil, errors.New("validationWorkDir is required")
	}

	return impl, nil
}

// ValidatePost handles the POST /validate
func (api *validatorAPIImpl) ValidatePost(c *gin.Context) {
	request := new(validatorAPI.ValidateRequest)
	if err := c.ShouldBindJSON(request); err != nil {
		log.Errorf("failed to bind request to json error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validatorsToBeExecuted, needFilesMounted := api.findValidatorsToBeExecuted(request.Validators)

	rsp := api.prepareValidateResponse(request.Validators, validatorsToBeExecuted)

	jobDir := filepath.Join(api.validationWorkDir, "jobs", uuid.NewString())

	if err := os.MkdirAll(jobDir, 0755); err != nil {
		log.Errorf("failed to create job dir, user: %s, error: %v", request.UserId, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := os.RemoveAll(jobDir); err != nil {
			log.Errorf("failed to clean up directory %s, error: %v", jobDir, err)
		}
	}()

	results := make(map[string]*validationOutput)

	userFiles, err := api.getUserFiles(jobDir, request.UserId, request.FilePaths, needFilesMounted)
	if err != nil {
		log.Errorf("failed to get user files, user: %s, error: %v", request.UserId, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Prep directories, inputs, etc and run each validator, and then collect the result
	for validatorPath, validatorDescription := range validatorsToBeExecuted {

		output, err := api.executeValidator(validatorPath, jobDir, validatorDescription, userFiles)

		if err != nil {
			log.Errorf("failed to execute validator %s, error: %v", validatorPath, err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		results[validatorDescription.ValidatorId] = output
	}

	for validator, result := range results {
		r := validatorOutputToValidateResponse(validator, result)
		rsp = append(rsp, *r)
	}

	c.JSON(200, rsp)
}

func validatorOutputToValidateResponse(validator string, output *validationOutput) *validatorAPI.ValidateResponseInner {
	r := &validatorAPI.ValidateResponseInner{
		Validator: validator,
		Result:    output.Result,
		Files:     make([]validatorAPI.ValidateResponseInnerFilesInner, len(output.Files)),
		Messages:  make([]validatorAPI.ValidateResponseInnerFilesInnerMessagesInner, len(output.Messages)),
	}

	for i, fileResult := range output.Files {
		filePath, _ := strings.CutPrefix(fileResult.Path, "/mnt/input/data")
		fileDetails := validatorAPI.ValidateResponseInnerFilesInner{
			Path:     filePath,
			Result:   fileResult.Result,
			Messages: make([]validatorAPI.ValidateResponseInnerFilesInnerMessagesInner, len(fileResult.Messages)),
		}

		for j, fileMessage := range fileResult.Messages {
			fileDetails.Messages[j] = validatorAPI.ValidateResponseInnerFilesInnerMessagesInner{
				Level:   fileMessage.Level,
				Time:    fileMessage.Time,
				Message: fileMessage.Message,
			}
		}

		r.Files[i] = fileDetails
	}
	for i, message := range output.Messages {
		r.Messages[i] = validatorAPI.ValidateResponseInnerFilesInnerMessagesInner{
			Level:   message.Level,
			Time:    message.Time,
			Message: message.Message,
		}
	}
	return r
}

func (api *validatorAPIImpl) executeValidator(validatorPath, jobDir string, validatorDescription *describeResult, userFiles []*userFileDetails) (*validationOutput, error) {
	validatorJobDir := filepath.Join(jobDir, validatorDescription.ValidatorId)

	if err := os.MkdirAll(validatorJobDir, 0755); err != nil {
		return nil, errors.Join(errors.New("failed to prepare directory for job"), err)
	}

	if err := os.MkdirAll(filepath.Join(validatorJobDir, "/input"), 0755); err != nil {
		return nil, errors.Join(errors.New("failed to prepare input directory for job"), err)
	}

	if err := os.MkdirAll(filepath.Join(validatorJobDir, "/output"), 0755); err != nil {
		return nil, errors.Join(errors.New("failed to prepare output directory for job"), err)
	}

	input := &validationInput{
		Files:  nil,
		Paths:  nil,
		Config: nil,
	}

	for _, userFile := range userFiles {
		filePathForJob := filepath.Join("/mnt/input/data", userFile.inboxPath)
		switch validatorDescription.Mode {
		case "file", "file-pair":
			input.Files = append(input.Files, &fileInput{Path: filePathForJob})
		case "file-structure":
			input.Paths = append(input.Paths, filePathForJob)
		}
	}

	inputData, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Join(errors.New("failed to marshal input struct to json for validator"), err)
	}
	if err := os.WriteFile(filepath.Join(validatorJobDir, "/input/input.json"), inputData, 0644); err != nil {
		return nil, errors.Join(errors.New("failed to write input.json for validator"), err)
	}

	// Here we monut the validatorJobDir as /mnt with the input, and output directories such that validator can access input/input.json and write a output/result.json
	// we also mount validatorJobDir/files/ as /mnt/input/data such that the validator can access the files without the need for us to duplicate them per validator
	// TODO ensure a validator can not modify the files to be validated
	_, err = api.commandExecutor.Execute(
		"apptainer",
		"run",
		"--bind",
		fmt.Sprintf("%s:/mnt", validatorJobDir),
		"--bind",
		fmt.Sprintf("%s:/mnt/input/data", filepath.Join(jobDir, "/files/")),
		validatorPath)
	if err != nil {
		return nil, errors.Join(errors.New("failed to execute run command"), err)
	}

	result, err := os.ReadFile(filepath.Join(validatorJobDir, "/output/result.json"))
	if err != nil {
		return nil, errors.Join(errors.New("failed to read result file"), err)
	}

	output := new(validationOutput)
	if err := json.Unmarshal(result, output); err != nil {
		return nil, errors.Join(errors.New("failed to unmarshal result"), err)
	}
	return output, nil
}

func (api *validatorAPIImpl) findValidatorsToBeExecuted(requestedValidators []string) (map[string]*describeResult, bool) {

	validatorsToBeExecuted := make(map[string]*describeResult)
	// If all validators requested to be used are of mode 'file-structure' we do not need to download, decrypt, and mount files
	needFilesMounted := false

	for _, path := range api.validatorPaths {
		out, err := api.commandExecutor.Execute(path, "describe")
		if err != nil {
			log.Errorf("failed to execute describe command towards path: %s, error: %v", path, err)
			continue
		}

		dr := new(describeResult)
		if err := json.Unmarshal(out, dr); err != nil {
			log.Errorf("failed to unmarshal response from describe command towards path: %s, error: %v", path, err)
			continue
		}

		for _, validatorName := range requestedValidators {
			if dr.ValidatorId == validatorName {
				validatorsToBeExecuted[path] = dr
				if dr.Mode != "file-structure" {
					needFilesMounted = true
				}
				break
			}
		}
	}

	return validatorsToBeExecuted, needFilesMounted
}

func (api *validatorAPIImpl) prepareValidateResponse(requestedValidators []string, validatorsToBeExecuted map[string]*describeResult) []validatorAPI.ValidateResponseInner {
	rsp := make([]validatorAPI.ValidateResponseInner, 0, len(requestedValidators))

	for _, validatorName := range requestedValidators {
		validatorFound := false

		for _, validatorToBeExecuted := range validatorsToBeExecuted {
			if validatorName == validatorToBeExecuted.ValidatorId {
				validatorFound = true
				break
			}
		}

		if validatorFound {
			continue
		}

		rsp = append(rsp, validatorAPI.ValidateResponseInner{
			Validator: validatorName,
			Result:    "failed",
			Files:     nil,
			Messages: []validatorAPI.ValidateResponseInnerFilesInnerMessagesInner{
				{
					Level:   "error",
					Time:    time.Now().String(),
					Message: "validator is not supported",
				},
			},
		})
	}
	return rsp
}

// getUserFiles, list all th users inbox files, find the id of the files requested to be validated, and then downloads the files
// to the jobDir such that they can be made available to the validators
func (api *validatorAPIImpl) getUserFiles(jobDir, userId string, filePaths []string, mountFiles bool) ([]*userFileDetails, error) {

	jobFilesDir := filepath.Join(jobDir, "/files/")

	if err := os.MkdirAll(jobFilesDir, 0755); err != nil {
		log.Errorf("failed to prepare files directory for job: %s", jobDir)
		return nil, err
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/files", api.sdaApiUrl, userId), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// TODO how to handle auth in better way
	req.Header.Add("Authorization", "Bearer "+api.sdaApiToken)
	req.Header.Add("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get response, reason: %v", err)
	}
	defer res.Body.Close()

	// Check the status code
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: url: %s", res.StatusCode, req.URL.String())
	}

	// Read the response body
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, reason: %v", err)
	}

	// Only get fields which are of interest
	type userFilesResponse struct {
		FileID    string `json:"fileID"`
		InboxPath string `json:"inboxPath"`
	}
	var userFiles []*userFilesResponse

	if err := json.Unmarshal(resBody, &userFiles); err != nil {
		return nil, fmt.Errorf("failed to uncmarshal response body, reason: %v", err)
	}

	var fileDetails []*userFileDetails

	for _, userFile := range userFiles {
		userFile.InboxPath = strings.TrimSuffix(userFile.InboxPath, ".c4gh")
		for _, filePath := range filePaths {
			if filePath == userFile.InboxPath {
				fileDetails = append(fileDetails, &userFileDetails{
					fileID:    userFile.FileID,
					inboxPath: userFile.InboxPath,
				})
				break
			}
		}
	}

	if len(fileDetails) == 0 {
		return fileDetails, nil
	}

	if !mountFiles {
		return fileDetails, nil
	}

	if err := api.downloadFiles(userId, jobFilesDir, fileDetails); err != nil {
		return nil, err
	}

	return fileDetails, nil

}
func (api *validatorAPIImpl) downloadFiles(userId, jobFilesDir string, fileDetails []*userFileDetails) error {
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
	for _, fileDetail := range fileDetails {
		if err := api.downloadFile(userId, jobFilesDir, fileDetail, pubKeyBase64, privateKeyData); err != nil {
			return err
		}
	}

	return nil
}

func (api *validatorAPIImpl) downloadFile(userId, jobFilesDir string, fileDetail *userFileDetails, pubKeyBase64 string, privateKeyData [32]byte) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/file/%s", api.sdaApiUrl, userId, fileDetail.fileID), nil)
	if err != nil {
		return fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// TODO how to handle auth in better way
	req.Header.Add("Authorization", "Bearer "+api.sdaApiToken)
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

	fileLocalPath := filepath.Join(jobFilesDir, fileDetail.inboxPath)

	dir, _ := filepath.Split(fileLocalPath)

	// Ensure any sub directories file is in is created
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Errorf("failed to create sub directory for file: %s, error: %v", dir, err)
		return err
	}

	file, err := os.Create(fileLocalPath)
	if err != nil {
		log.Errorf("failed to create file for file: %s, error: %v", fileLocalPath, err)
		return err
	}

	// Decrypt file

	crypt4GHReader, err := streaming.NewCrypt4GHReader(res.Body, privateKeyData, nil)
	if err != nil {
		_ = res.Body.Close()
		_ = file.Close()
		return fmt.Errorf("could not create cryp4gh reader: %v", err)
	}

	_, err = io.Copy(file, crypt4GHReader)
	if err != nil {
		_ = res.Body.Close()
		_ = crypt4GHReader.Close()
		_ = file.Close()
		return fmt.Errorf("could not decrypt file %s: %s", fileDetail.inboxPath, err)
	}
	_ = res.Body.Close()
	_ = crypt4GHReader.Close()
	_ = file.Close()

	return nil
}

type userFileDetails struct {
	fileID    string
	inboxPath string
}

// ValidatorsGet handles the GET /validators
func (api *validatorAPIImpl) ValidatorsGet(c *gin.Context) {

	rsp := make([]string, 0)

	for _, path := range api.validatorPaths {

		out, err := api.commandExecutor.Execute(path, "describe")
		if err != nil {
			log.Errorf("failed to execute describe command towards path: %s, error: %v", path, err)
			continue
		}

		dr := new(describeResult)
		if err := json.Unmarshal(out, dr); err != nil {
			log.Errorf("failed to unmarshal response from describe command towards path: %s, error: %v", path, err)
			continue
		}

		rsp = append(rsp, dr.ValidatorId)

	}

	c.JSON(200, rsp)
}

type validationInput struct {
	Files  []*fileInput `json:"files"`
	Paths  []string     `json:"paths"`
	Config *config      `json:"config"`
}
type fileInput struct {
	Path string `json:"path"`
}
type config struct {
}
type validationOutput struct {
	Result   string           `json:"result"`
	Files    []*fileOutput    `json:"files"`
	Messages []*outPutMessage `json:"messages"`
}
type fileOutput struct {
	Path     string           `json:"path"`
	Result   string           `json:"result"`
	Messages []*outPutMessage `json:"messages"`
}
type outPutMessage struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}
type describeResult struct {
	ValidatorId       string   `json:"validatorId"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Version           string   `json:"version"`
	Mode              string   `json:"mode"`
	PathSpecification []string `json:"pathSpecification"`
}
