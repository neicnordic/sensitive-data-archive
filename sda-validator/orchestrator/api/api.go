package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
	openapi "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/api/openapi_interface"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/database"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/model"
	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/validators"
	log "github.com/sirupsen/logrus"
)

type validatorAPIImpl struct {
	sdaAPIURL                     string
	sdaAPIToken                   string
	validationFileSizeLimit       int64
	validationJobPreparationQueue string
	broker                        broker.AMQPBrokerI
}

func NewValidatorAPIImpl(options ...func(*validatorAPIImpl)) (openapi.ValidatorOrchestratorAPI, error) {
	impl := &validatorAPIImpl{}

	for _, option := range options {
		option(impl)
	}

	if impl.sdaAPIURL == "" {
		return nil, errors.New("sdaAPIURL is required")
	}
	if impl.sdaAPIToken == "" {
		return nil, errors.New("sdaAPIToken is required")
	}
	if impl.validationFileSizeLimit == 0 {
		return nil, errors.New("validationFileSizeLimit is required")
	}
	if impl.validationJobPreparationQueue == "" {
		return nil, errors.New("validationJobPreparationQueue is required")
	}
	if impl.broker == nil {
		return nil, errors.New("broker is required")
	}

	return impl, nil
}

func (api *validatorAPIImpl) AdminResultGet(c *gin.Context) {
	api.result(c, c.Query("validation_id"), nil)
}

func (api *validatorAPIImpl) ResultGet(c *gin.Context) {
	token, ok := c.Get("token")
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	userID := token.(jwt.Token).Subject()

	api.result(c, c.Query("validation_id"), &userID)
}

func (api *validatorAPIImpl) result(c *gin.Context, validationID string, userID *string) {

	if _, err := uuid.Parse(validationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid validation id: %s", validationID)})

		return
	}

	validationResult, err := database.ReadValidationResult(c, validationID, userID)

	if err != nil {
		log.Errorf("failed to read validation result: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	if validationResult == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("No validation with id: %s found for the given user", validationID)})
		return
	}

	var rsp []*openapi.ResultResponseInner

	for _, validatorResult := range validationResult.ValidatorResults {
		vr := &openapi.ResultResponseInner{
			ValidatorId: validatorResult.ValidatorID,
			Result:      validatorResult.Result,
			StartedAt:   validatorResult.StartedAt,
			FinishedAt:  validatorResult.FinishedAt,
			Files:       make([]openapi.ResultResponseInnerFilesInner, len(validatorResult.Files)),
			Messages:    make([]openapi.ResultResponseInnerFilesInnerMessagesInner, len(validatorResult.Messages)),
		}

		for i, fileResult := range validatorResult.Files {
			fr := openapi.ResultResponseInnerFilesInner{
				Path:     fileResult.FilePath,
				Result:   fileResult.Result,
				Messages: make([]openapi.ResultResponseInnerFilesInnerMessagesInner, len(fileResult.Messages)),
			}
			for j, message := range fileResult.Messages {
				fr.Messages[j] = openapi.ResultResponseInnerFilesInnerMessagesInner{
					Level:   message.Level,
					Time:    message.Time,
					Message: message.Message,
				}
			}
			vr.Files[i] = fr
		}

		for j, message := range validatorResult.Messages {
			vr.Messages[j] = openapi.ResultResponseInnerFilesInnerMessagesInner{
				Level:   message.Level,
				Time:    message.Time,
				Message: message.Message,
			}
		}

		rsp = append(rsp, vr)
	}

	c.JSON(200, rsp)
}

func (api *validatorAPIImpl) AdminValidatePost(c *gin.Context) {
	token, ok := c.Get("token")
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	userID := token.(jwt.Token).Subject()
	request := new(openapi.AdminValidateRequest)
	if err := c.ShouldBindJSON(request); err != nil {
		log.Errorf("failed to bind request to json error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

		return
	}

	api.validate(c, request.UserId, userID, request.FilePaths, request.Validators)
}

// ValidatePost handles the POST /validate
func (api *validatorAPIImpl) ValidatePost(c *gin.Context) {
	token, ok := c.Get("token")
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	userID := token.(jwt.Token).Subject()
	request := new(openapi.ValidateRequest)
	if err := c.ShouldBindJSON(request); err != nil {
		log.Errorf("failed to bind request to json error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

		return
	}

	api.validate(c, userID, userID, request.FilePaths, request.Validators)
}

func (api *validatorAPIImpl) validate(c *gin.Context, userID, triggeredBy string, requestedFilePaths, requestedValidators []string) {
	var unsupportedValidators []string
	var requiresFileContent bool
	for _, requestedValidator := range requestedValidators {
		validatorDescription, ok := validators.Validators[requestedValidator]
		if !ok {
			unsupportedValidators = append(unsupportedValidators, requestedValidator)

			continue
		}
		requiresFileContent = validatorDescription.RequiresFileContent() || requiresFileContent
	}
	if len(unsupportedValidators) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%v are not supported validators", unsupportedValidators)})

		return
	}

	userFiles, err := api.getUserFiles(userID, requestedFilePaths)
	if err != nil {
		log.Errorf("failed to get user files due to: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	if len(userFiles.missingFiles) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("files: %v not found", userFiles.missingFiles)})

		return
	}

	if requiresFileContent && userFiles.sumFilesSize > api.validationFileSizeLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "requested files exceed the file size limit"})

		return
	}

	validationID := uuid.NewString()

	tx, err := database.BeginTransaction(c)
	if err != nil {
		log.Errorf("failed to begin transaction due to: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Errorf("failed to rollback transactions due to: %v", err)
		}
	}()

	now := time.Now()
	for _, validatorID := range requestedValidators {
		for _, file := range userFiles.fileInformation {
			if err := tx.InsertFileValidationJob(c, &model.InsertFileValidationJobParameters{
				ValidationID:       validationID,
				ValidatorID:        validatorID,
				FileID:             file.FileID,
				FilePath:           file.FilePath,
				SubmissionUser:     userID,
				TriggeredBy:        triggeredBy,
				FileSubmissionSize: file.SubmissionFileSize,
				StartedAt:          now,
			}); err != nil {
				log.Errorf("failed to insert file validation job due to: %v", err)
				c.AbortWithStatus(http.StatusInternalServerError)

				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		log.Errorf("failed to commit the transaction due to: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	jobPreparationMessage := &model.JobPreparationMessage{ValidationID: validationID}
	msg, err := json.Marshal(jobPreparationMessage)
	if err != nil {
		log.Errorf("failed to marshal job perparation message due to: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	if err := api.broker.PublishMessage(c, api.validationJobPreparationQueue, msg); err != nil {
		log.Errorf("failed to publish job perparation message due to: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)

		return
	}

	c.JSON(200, &openapi.ValidatePost200Response{ValidationId: validationID})
}

type getUserFilesResponse struct {
	fileInformation map[string]*model.FileInformation
	sumFilesSize    int64
	missingFiles    []string
}

func (api *validatorAPIImpl) getUserFiles(userID string, requestedFilePaths []string) (*getUserFilesResponse, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/files", api.sdaAPIURL, userID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// TODO how to handle auth in better way, TBD #989
	req.Header.Add("Authorization", "Bearer "+api.sdaAPIToken)
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

	var userFiles []*model.UserFilesResponse

	if err := json.Unmarshal(resBody, &userFiles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, reason: %v", err)
	}

	rsp := &getUserFilesResponse{
		fileInformation: make(map[string]*model.FileInformation),
	}

	for _, filePath := range requestedFilePaths {
		fileFound := false
		for _, userFile := range userFiles {
			userFile.InboxPath = strings.TrimSuffix(userFile.InboxPath, ".c4gh")
			if filePath == userFile.InboxPath {
				rsp.fileInformation[filePath] = &model.FileInformation{
					FileID:             userFile.FileID,
					FilePath:           userFile.InboxPath,
					SubmissionFileSize: userFile.SubmissionFileSize,
				}
				fileFound = true
				rsp.sumFilesSize += userFile.SubmissionFileSize

				break
			}
		}
		if !fileFound {
			rsp.missingFiles = append(rsp.missingFiles, filePath)
		}
	}

	return rsp, nil
}

func (api *validatorAPIImpl) ValidatorsGet(c *gin.Context) {
	rsp := make([]string, 0)

	for validatorID := range validators.Validators {
		rsp = append(rsp, validatorID)
	}

	c.JSON(200, rsp)
}
