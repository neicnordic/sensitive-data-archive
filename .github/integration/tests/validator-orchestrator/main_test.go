package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite

	// Config
	url             string
	inboxURL        string
	jwtKeyFilePath  string
	c4ghKeyFilePath string
	s3AccessKey     string
	s3SecretKey     string

	adminToken, submissionToken string
}

func TestTestSuite(m *testing.T) {
	suite.Run(m, new(TestSuite))
}

func (ts *TestSuite) SetupSuite() {
	// Note these can be changed if you want to test against different endpoints, etc
	ts.url = "http://validator-orchestrator:8080"
	ts.inboxURL = "http://s3inbox:8000"
	ts.jwtKeyFilePath = "/shared/keys/jwt.key"
	ts.c4ghKeyFilePath = "/shared/c4gh.pub.pem"
	ts.s3AccessKey = "submission_user_integration.test"
	ts.s3SecretKey = "secretKey"

	var err error
	ts.adminToken, err = ts.generateToken("admin_user@integration.test")
	if err != nil {
		ts.FailNow("failed to generate admin token", err.Error())
	}

	ts.submissionToken, err = ts.generateToken("submission_user@integration.test")
	if err != nil {
		ts.FailNow("failed to generate submission token", err.Error())
	}

	if err := ts.uploadFiles(); err != nil {
		ts.FailNow("failed to upload files", err.Error())
	}
}

func (ts *TestSuite) uploadFiles() error {
	log.Println("uploading files")

	httpClient := http.DefaultClient

	awsConf, err := awsConfig.LoadDefaultConfig(context.Background(),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			ts.s3AccessKey,
			ts.s3SecretKey,
			ts.submissionToken,
		)),
		awsConfig.WithRegion("us-east-1"),
		awsConfig.WithBaseEndpoint(ts.inboxURL),
	)
	if err != nil {
		return fmt.Errorf("failed to load aws config: %v", err)
	}

	s3Client := s3.NewFromConfig(awsConf, func(o *s3.Options) {
		o.UsePathStyle = true
		o.EndpointOptions.DisableHTTPS = true
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	// Create an uploader with the session and default options
	uploader := manager.NewUploader(s3Client)

	files := []string{"NA12878.bam", "NA12878_20k_b37.bam", "NA12878.bai", "NA12878_20k_b37.bai"}
	for _, fileName := range files {
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://github.com/ga4gh/htsget-refserver/raw/main/data/gcp/gatk-test-data/wgs_bam/%s", fileName), nil)
		req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

		rsp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to do http request: %v", err)
		}

		c4ghKey, err := os.ReadFile(ts.c4ghKeyFilePath)
		if err != nil {
			return fmt.Errorf("failed to read c4gh file: %v", err)
		}

		keyReader := bytes.NewReader(c4ghKey)
		newReaderPublicKey, err := keys.ReadPublicKey(keyReader)
		if err != nil {
			return fmt.Errorf("failed to public jwt key file: %v", err)
		}

		encryptedWriter := bytes.Buffer{}
		encryptedFileWriter, err := streaming.NewCrypt4GHWriter(&encryptedWriter, [32]byte{}, [][32]byte{newReaderPublicKey}, nil)
		if err != nil {
			return fmt.Errorf("failed to create c4gh writer: %v", err)
		}

		_, _ = io.Copy(encryptedFileWriter, rsp.Body)
		_ = encryptedFileWriter.Close()
		_ = rsp.Body.Close()

		reader := bytes.NewReader(encryptedWriter.Bytes())

		_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
			Body:            reader,
			Bucket:          aws.String(ts.s3AccessKey),
			Key:             aws.String(fileName),
			ContentEncoding: aws.String("UTF-8"),
		}, func(u *manager.Uploader) {
			u.PartSize = 50 * 1024 * 1024
			u.LeavePartsOnError = false
		})
		if err != nil {
			return fmt.Errorf("failed to upload file: %v", err)
		}
	}

	return nil
}

func (ts *TestSuite) generateToken(sub string) (string, error) {
	keyPem, err := os.ReadFile(ts.jwtKeyFilePath)
	if err != nil {
		ts.FailNow(fmt.Sprintf("failed to read key file from path: %s", ts.jwtKeyFilePath), err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &jwt.RegisteredClaims{
		Subject:   sub,
		Issuer:    "http://integration.test",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Second * 30)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})

	token.Header["kid"] = "rsa1"
	block, _ := pem.Decode(keyPem)
	keyRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}

	return token.SignedString(keyRaw)
}

func (ts *TestSuite) TestValidatorsAsAdmin() {
	httpClient := http.DefaultClient

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/validators", ts.url), nil)
	req.Header.Add("Authorization", "Bearer "+string(ts.adminToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
		return
	}

	// Read the response body
	resBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		ts.FailNow("failed to read response body", err.Error())
	}

	ts.Equal(http.StatusOK, rsp.StatusCode)
	ts.Contains(string(resBody), "validator-word-count-100-200")
	ts.Contains(string(resBody), "validator-always-success")
}
func (ts *TestSuite) TestValidatorsAsSubmitter() {
	httpClient := http.DefaultClient

	req, _ := http.NewRequest("GET", fmt.Sprint(ts.url, "/validators"), nil)
	req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	// Read the response body
	resBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		ts.FailNow("failed to read response body", err.Error())
	}

	ts.Equal(http.StatusOK, rsp.StatusCode)
	ts.Contains(string(resBody), "validator-word-count-100-200")
	ts.Contains(string(resBody), "validator-always-success")
}
func (ts *TestSuite) TestValidateAndResult() {
	httpClient := http.DefaultClient

	files := `["NA12878.bam", "NA12878_20k_b37.bam", "NA12878.bai", "NA12878_20k_b37.bai"]`
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/validate", ts.url), bytes.NewReader([]byte(fmt.Sprintf(`{
"file_paths": %s,
"validators": ["validator-word-count-100-200", "validator-always-success"]
}`, files))))
	req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	resBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		ts.FailNow("failed to read response body", err.Error())
	}

	_ = rsp.Body.Close()

	parsedResponse := new(validateResponse)

	ts.Equal(http.StatusOK, rsp.StatusCode)

	if err := json.Unmarshal(resBody, &parsedResponse); err != nil {
		ts.FailNow("failed to unmarshal response body", err.Error())
	}

	if _, err := uuid.Parse(parsedResponse.ValidationId); err != nil {
		ts.Fail("failed to parse validation id in response as a uuid", err.Error())
	}

	// poll until result is no longer pending
	var resultResponse []validatorResult

	abortPollingAt := time.Now().Add(time.Second * 30)

	for {
		req, _ = http.NewRequest("GET", fmt.Sprintf("%s/result?validation_id=%s", ts.url, parsedResponse.ValidationId), nil)
		req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

		rsp, err = httpClient.Do(req)
		if err != nil {
			ts.FailNow("failed to DO http request", err.Error())
		}

		ts.Equal(http.StatusOK, rsp.StatusCode)

		resBody, err = io.ReadAll(rsp.Body)
		if err != nil {
			ts.FailNow("failed to read response body", err.Error())
		}
		_ = rsp.Body.Close()

		if err := json.Unmarshal(resBody, &resultResponse); err != nil {
			ts.FailNow("failed to parse result response body", err.Error())
		}

		allDone := true
		for _, result := range resultResponse {
			if result.Result == "pending" {
				allDone = false
				break
			}
		}
		if !allDone {
			if time.Now().After(abortPollingAt) {
				ts.FailNow("validate result did not complete and give non pending results within 30 seconds")
			}
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	ts.Len(resultResponse, 2)

	for _, result := range resultResponse {
		ts.Len(result.Files, 4)
		switch result.ValidatorId {
		case "validator-word-count-100-200":

			ts.Len(result.Messages, 0)
			ts.Equal("failed", result.Result)

			for _, fr := range result.Files {
				ts.Equal("failed", fr.Result)
				ts.Contains(files, fr.Path)
				ts.Len(fr.Messages, 1)
			}

		case "validator-always-success":
			ts.Len(result.Messages, 0)
			ts.Equal("passed", result.Result)

			for _, fr := range result.Files {
				ts.Equal("passed", fr.Result)
				ts.Contains(files, fr.Path)
				ts.Len(fr.Messages, 0)
			}

		default:
			ts.Fail("unexpected validator id in result")
		}
	}
}
func (ts *TestSuite) TestAdminValidateAndAdminResult() {
	httpClient := http.DefaultClient

	files := `["NA12878.bam", "NA12878_20k_b37.bam", "NA12878.bai", "NA12878_20k_b37.bai"]`
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/validate", ts.url), bytes.NewReader([]byte(fmt.Sprintf(`{
"file_paths": %s,
"validators": ["validator-word-count-100-200", "validator-always-success"],
"user_id": "submission_user@integration.test"
}`, files))))
	req.Header.Add("Authorization", "Bearer "+string(ts.adminToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	resBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		ts.FailNow("failed to read response body", err.Error())
	}

	_ = rsp.Body.Close()

	parsedResponse := new(validateResponse)

	ts.Equal(http.StatusOK, rsp.StatusCode)

	if err := json.Unmarshal(resBody, &parsedResponse); err != nil {
		ts.FailNow("failed to unmarshal response body", err.Error())
	}

	if _, err := uuid.Parse(parsedResponse.ValidationId); err != nil {
		ts.Fail("failed to parse validation id in response as a uuid", err.Error())
	}

	// poll until result is no longer pending
	var resultResponse []validatorResult

	abortPollingAt := time.Now().Add(time.Second * 30)

	for {
		req, _ = http.NewRequest("GET", fmt.Sprintf("%s/admin/result?validation_id=%s&submission_user=submission_user@integration.test", ts.url, parsedResponse.ValidationId), nil)
		req.Header.Add("Authorization", "Bearer "+string(ts.adminToken))

		rsp, err = httpClient.Do(req)
		if err != nil {
			ts.FailNow("failed to DO http request", err.Error())
		}

		ts.Equal(http.StatusOK, rsp.StatusCode)

		resBody, err = io.ReadAll(rsp.Body)
		if err != nil {
			ts.FailNow("failed to read response body", err.Error())
		}
		_ = rsp.Body.Close()

		if err := json.Unmarshal(resBody, &resultResponse); err != nil {
			ts.FailNow("failed to parse result response body", err.Error())
		}

		allDone := true
		for _, result := range resultResponse {
			if result.Result == "pending" {
				allDone = false
				break
			}
		}
		if !allDone {
			if time.Now().After(abortPollingAt) {
				ts.FailNow("validate result did not complete and give non pending results within 30 seconds")
			}
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	ts.Len(resultResponse, 2)

	for _, result := range resultResponse {
		ts.Len(result.Files, 4)
		switch result.ValidatorId {
		case "validator-word-count-100-200":

			ts.Len(result.Messages, 0)
			ts.Equal("failed", result.Result)

			for _, fr := range result.Files {
				ts.Equal("failed", fr.Result)
				ts.Contains(files, fr.Path)
				ts.Len(fr.Messages, 1)
			}

		case "validator-always-success":
			ts.Len(result.Messages, 0)
			ts.Equal("passed", result.Result)

			for _, fr := range result.Files {
				ts.Equal("passed", fr.Result)
				ts.Contains(files, fr.Path)
				ts.Len(fr.Messages, 0)
			}

		default:
			ts.Fail("unexpected validator id in result")
		}
	}
}

func (ts *TestSuite) TestAdminValidateAsSubmitter() {
	httpClient := http.DefaultClient

	files := `["NA12878.bam", "NA12878_20k_b37.bam", "NA12878.bai", "NA12878_20k_b37.bai"]`
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/validate", ts.url), bytes.NewReader([]byte(fmt.Sprintf(`{
"file_paths": %s,
"validators": ["validator-word-count-100-200", "validator-always-success"],
"user_id": "submission_user@integration.test"
}`, files))))
	req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	_ = rsp.Body.Close()
	ts.Equal(http.StatusUnauthorized, rsp.StatusCode)
}

func (ts *TestSuite) TestAdminResultAsSubmitter() {
	httpClient := http.DefaultClient

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/result?validation_id=dont_matter", ts.url), nil)
	req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	_ = rsp.Body.Close()
	ts.Equal(http.StatusUnauthorized, rsp.StatusCode)
}

func (ts *TestSuite) TestValidateUnknownFile() {
	httpClient := http.DefaultClient

	files := `["FILE_NO_EXISTS.xml", "dir/FILE_NO_EXISTS_1.xml", "NA12878.bai", "NA12878_20k_b37.bai"]`
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/validate", ts.url), bytes.NewReader([]byte(fmt.Sprintf(`{
"file_paths": %s,
"validators": ["validator-word-count-100-200", "validator-always-success"]
}`, files))))
	req.Header.Add("Authorization", "Bearer "+string(ts.submissionToken))

	rsp, err := httpClient.Do(req)
	if err != nil {
		ts.FailNow("failed to DO http request", err.Error())
	}

	resBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		ts.FailNow("failed to read response body", err.Error())
	}
	_ = rsp.Body.Close()
	ts.Equal(http.StatusBadRequest, rsp.StatusCode)
	ts.Equal(`{"error":"files: [FILE_NO_EXISTS.xml dir/FILE_NO_EXISTS_1.xml] not found"}`, string(resBody))
}

type validateResponse struct {
	ValidationId string `json:"validation_id,omitempty"`
}

type validatorResult struct {
	ValidatorId string          `json:"validator_id,omitempty"`
	Result      string          `json:"result,omitempty"`
	StartedAt   time.Time       `json:"started_at,omitempty"`
	FinishedAt  time.Time       `json:"finished_at,omitempty"`
	Files       []fileResult    `json:"files,omitempty"`
	Messages    []resultMessage `json:"messages,omitempty"`
}

type fileResult struct {
	Path     string          `json:"path,omitempty"`
	Result   string          `json:"result,omitempty"`
	Messages []resultMessage `json:"messages,omitempty"`
}

type resultMessage struct {
	Level   string `json:"level,omitempty"`
	Time    string `json:"time,omitempty"`
	Message string `json:"message,omitempty"`
}
