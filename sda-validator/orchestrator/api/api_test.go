package api

import (
	"bytes"
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

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	validatorAPI "github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/openapi/go-gin-server/go"
	"github.com/stretchr/testify/suite"
)

type ValidatorAPITestSuite struct {
	suite.Suite

	tempDir string
}

func (ts *ValidatorAPITestSuite) SetupTest() {
	ts.tempDir = ts.T().TempDir()
}
func (ts *ValidatorAPITestSuite) TearDownTest() {
}

func TestValidatorAPITestSuite(t *testing.T) {
	suite.Run(t, new(ValidatorAPITestSuite))
}

type mockCommandExecutor struct {
	passValidation bool
}

func (mce mockCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("no args for mock command executor")
	}

	if args[0] == "describe" {
		return json.Marshal(&describeResult{
			ValidatorId:       strings.TrimSuffix(name, "-path"),
			Name:              "mock validator",
			Description:       "Mock validator description",
			Version:           "v0.0.0",
			Mode:              "file",
			PathSpecification: []string{"*"},
		})
	}

	// Currently we expect 6 args when running a validator
	// as seen at api.executeValidator(...)
	if len(args) != 6 {
		return nil, errors.New("unexpected amount of args when running validator")
	}

	// extracting path being mounted as /mnt and writing directly in that directory
	jobDir := strings.Split(args[2], ":")[0]

	inputRaw, err := os.ReadFile(filepath.Join(jobDir, "input", "input.json"))
	if err != nil {
		return nil, fmt.Errorf("mock validator could not read input file, error: %v", err)
	}

	input := new(validationInput)
	if err := json.Unmarshal(inputRaw, input); err != nil {
		return nil, fmt.Errorf("mock validator could not unmarshal input, error: %v", err)
	}

	output := &validationOutput{
		Result:   "passed",
		Files:    nil,
		Messages: nil,
	}

	if !mce.passValidation {
		output.Result = "failed"
		output.Messages = []*outPutMessage{
			{
				Level:   "error",
				Time:    time.Now().String(),
				Message: "mock validator failing validation",
			},
		}
	}

	for _, file := range input.Files {
		fo := &fileOutput{
			Path:     file.Path,
			Result:   "passed",
			Messages: nil,
		}

		if !mce.passValidation {
			fo.Result = "failed"
			fo.Messages = []*outPutMessage{
				{
					Level:   "error",
					Time:    time.Now().String(),
					Message: "mock validator failing validation",
				},
			}
		}

		output.Files = append(output.Files, fo)
	}

	outputRaw, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("mock validator could not marshal output, error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "output", "result.json"), outputRaw, 755); err != nil {
		return nil, fmt.Errorf("mock validator could not write output file, error: %v", err)
	}

	return nil, nil
}

func (ts *ValidatorAPITestSuite) TestNewValidatorAPIImpl_MissingSdaApiUrl() {
	_, err := NewValidatorAPIImpl(
		SdaApiToken("token"),
		ValidationWorkDir("workDir"),
	)

	ts.EqualError(err, "sdaApiUrl is required")
}

func (ts *ValidatorAPITestSuite) TestNewValidatorAPIImpl_MissingSdaApiToken() {
	_, err := NewValidatorAPIImpl(
		SdaApiUrl("url"),
		ValidationWorkDir("workDir"),
	)

	ts.EqualError(err, "sdaApiToken is required")
}
func (ts *ValidatorAPITestSuite) TestNewValidatorAPIImpl_MissingValidationWorkDir() {
	_, err := NewValidatorAPIImpl(
		SdaApiUrl("url"),
		SdaApiToken("token"),
	)

	ts.EqualError(err, "validationWorkDir is required")
}

func (ts *ValidatorAPITestSuite) TestPrepareValidateResponse() {
	apiImpl := validatorAPIImpl{}

	preparedValidationResponse := apiImpl.prepareValidateResponse([]string{"mock-validator-1", "mock-validator-2", "mock-validator-3"}, map[string]*describeResult{
		"mock-validator-1-path": {
			ValidatorId: "mock-validator-1",
		},
		"mock-validator-2-path": {
			ValidatorId: "mock-validator-2",
		},
		"mock-validator-3-path": {
			ValidatorId: "mock-validator-3",
		},
	})

	ts.Len(preparedValidationResponse, 0)
}
func (ts *ValidatorAPITestSuite) TestPrepareValidateResponse_MissingValidator() {
	apiImpl := validatorAPIImpl{}

	preparedValidationResponse := apiImpl.prepareValidateResponse([]string{"mock-validator-1", "mock-validator-2", "mock-validator-3"}, map[string]*describeResult{
		"mock-validator-1-path": {
			ValidatorId: "mock-validator-1",
		},
		"mock-validator-2-path": {
			ValidatorId: "mock-validator-2",
		},
	})

	ts.Len(preparedValidationResponse, 1)

}
func (ts *ValidatorAPITestSuite) TestFindValidatorsToBeExecuted() {
	apiImpl := validatorAPIImpl{
		validatorPaths:  []string{"mock-validator-1-path", "mock-validator-2-path", "mock-validator-3-path"},
		commandExecutor: &mockCommandExecutor{passValidation: false},
	}

	validatorsToBeExecuted, needFilesMounted := apiImpl.findValidatorsToBeExecuted([]string{"mock-validator-1", "mock-validator-2", "mock-validator-3"})

	if len(validatorsToBeExecuted) != 3 {
		ts.FailNow("did not find the expected number of validators to be executed")
	}
	ts.Equal(validatorsToBeExecuted["mock-validator-1-path"].ValidatorId, "mock-validator-1")
	ts.Equal(validatorsToBeExecuted["mock-validator-2-path"].ValidatorId, "mock-validator-2")
	ts.Equal(validatorsToBeExecuted["mock-validator-3-path"].ValidatorId, "mock-validator-3")
	ts.Equal(needFilesMounted, true)
}

func (ts *ValidatorAPITestSuite) TestPrepareUserFiles() {

	httpTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		switch {
		case req.RequestURI == "/users/test_user/files":
			// Set the response status code
			w.WriteHeader(http.StatusOK)
			// Set the response body
			_, _ = w.Write([]byte(`[
{
	"FileID": "test-file-id-1",
	"InboxPath": "testFile1"
},{
	"FileID": "test-file-id-2",
	"InboxPath": "testFile2"
},{
	"FileID": "test-file-id-3",
	"InboxPath": "testFile3"
},{
	"FileID": "test-file-id-4",
	"InboxPath": "test_dir/testFile4"
},{
	"FileID": "test-file-id-5",
	"InboxPath": "test_dir/testFile5"
}
]`))
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
			file.Write([]byte(fmt.Sprintf("this is file: %s", filepath.Base(req.URL.Path))))
			io.Copy(encryptedFileWriter, &file)

			encryptedFileWriter.Close()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(encryptedFile.String()))
		default:
			// Set the response status code
			w.WriteHeader(http.StatusInternalServerError)
			// Set the response body
			fmt.Fprint(w, "unexpected path called")
		}
	}))
	defer httpTestServer.Close()

	apiImpl := validatorAPIImpl{
		sdaApiUrl: httpTestServer.URL,
	}

	userFiles, missingUserfiles, err := apiImpl.prepareUserFiles(ts.tempDir, "test_user", []string{"testFile1", "testFile3", "test_dir/testFile5"}, true)

	ts.NoError(err)

	ts.Len(missingUserfiles, 0)
	ts.Len(userFiles, 3)

	for _, file := range userFiles {
		fileContent, err := os.ReadFile(filepath.Join(ts.tempDir, "files", file.inboxPath))
		ts.NoError(err)
		if err != nil {
			continue
		}
		ts.Equal(fmt.Sprintf("this is file: %s", file.fileID), string(fileContent))
	}
}

func (ts *ValidatorAPITestSuite) TestPrepareUserFiles_MissingFiles() {
	httpTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.RequestURI == "/users/test_user/files":
			// Set the response status code
			w.WriteHeader(http.StatusOK)
			// Set the response body
			_, _ = w.Write([]byte(`[]`))
		default:
			// Set the response status code
			w.WriteHeader(http.StatusInternalServerError)
			// Set the response body
			fmt.Fprint(w, "unexpected path called")
		}
	}))
	defer httpTestServer.Close()

	apiImpl := validatorAPIImpl{
		sdaApiUrl: httpTestServer.URL,
	}

	userFiles, missingUserFiles, err := apiImpl.prepareUserFiles(ts.tempDir, "test_user", []string{"testFile1", "testFile3", "test_dir/testFile5"}, true)

	ts.NoError(err)

	ts.Len(missingUserFiles, 3)
	ts.Len(userFiles, 0)

}

func (ts *ValidatorAPITestSuite) TestValidatorOutputToValidateResponse() {
	now := time.Now()

	validateResponseInner := validatorOutputToValidateResponse("mock-validator", &validationOutput{
		Result: "passed",
		Files: []*fileOutput{
			{
				Path:     "/mnt/input/data/test_file_1",
				Result:   "passed",
				Messages: nil,
			}, {
				Path:     "/mnt/input/data/test_file_2",
				Result:   "passed",
				Messages: nil,
			}, {
				Path:   "/mnt/input/data/test_dir/test_file_3",
				Result: "failed",
				Messages: []*outPutMessage{
					{
						Level:   "info",
						Time:    now.String(),
						Message: "file failed validation",
					},
				},
			},
		},
		Messages: []*outPutMessage{
			{
				Level:   "info",
				Time:    now.String(),
				Message: "2/3 files passed validation",
			},
		},
	})

	ts.Equal("mock-validator", validateResponseInner.Validator)

	ts.Equal("test_file_1", validateResponseInner.Files[0].Path)
	ts.Equal("passed", validateResponseInner.Files[0].Result)
	ts.Equal([]validatorAPI.ValidateResponseInnerFilesInnerMessagesInner{}, validateResponseInner.Files[0].Messages)

	ts.Equal("test_file_2", validateResponseInner.Files[1].Path)
	ts.Equal("passed", validateResponseInner.Files[1].Result)
	ts.Equal([]validatorAPI.ValidateResponseInnerFilesInnerMessagesInner{}, validateResponseInner.Files[1].Messages)

	ts.Equal("test_dir/test_file_3", validateResponseInner.Files[2].Path)
	ts.Equal("failed", validateResponseInner.Files[2].Result)
	ts.Equal("info", validateResponseInner.Files[2].Messages[0].Level)
	ts.Equal(now.String(), validateResponseInner.Files[2].Messages[0].Time)
	ts.Equal("file failed validation", validateResponseInner.Files[2].Messages[0].Message)

	ts.Equal("info", validateResponseInner.Messages[0].Level)
	ts.Equal(now.String(), validateResponseInner.Messages[0].Time)
	ts.Equal("2/3 files passed validation", validateResponseInner.Messages[0].Message)
}
