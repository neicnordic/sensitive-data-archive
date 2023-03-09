package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sda-pipeline/internal/common"
	"sda-pipeline/internal/config"

	smtpmock "github.com/mocktools/go-smtp-mock"
	"github.com/rabbitmq/amqp091-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) SetupTest() {
	viper.Set("log.level", "debug")
}

func TestGetUser(t *testing.T) {

	archivedMsg := common.Archived{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		FileID:      123456789,
		ArchivePath: "f25c51cb-c10b-44da-8021-d0fca7110219",
		EncryptedChecksums: []common.Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
		ReVerify: false,
	}

	archivedMsgBytes, _ := json.Marshal(archivedMsg)

	archivedUser := getUser("ready", archivedMsgBytes)
	assert.Equal(t, "JohnDoe", archivedUser)

	infoError := common.InfoError{
		Error:           "Failed to open file to ingest",
		Reason:          "This is an error",
		OriginalMessage: &archivedMsgBytes,
	}

	infoErrorBytes, _ := json.Marshal(infoError)

	orgUser := getUser("error", infoErrorBytes)
	assert.Equal(t, "JohnDoe", orgUser)

}

func TestSetSubject(t *testing.T) {
	assert.Equal(t, "Error during ingestion", setSubject("error"))
	assert.Equal(t, "Ingestion completed", setSubject("ready"))
	assert.Empty(t, setSubject("phail"))
}

func TestValidator(t *testing.T) {
	d := amqp091.Delivery{}

	archivedMsg := common.Archived{
		User:        "JohnDoe",
		FilePath:    "path/to file",
		FileID:      123456789,
		ArchivePath: "f25c51cb-c10b-44da-8021-d0fca7110219",
		EncryptedChecksums: []common.Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
		ReVerify: false,
	}

	orgMsg, _ := json.Marshal(archivedMsg)

	infoError := common.InfoError{
		Error:           "Failed to open file to ingest",
		Reason:          "This is an error",
		OriginalMessage: &orgMsg,
	}

	d.Body, _ = json.Marshal(infoError)
	err := validator("error", "file://../../schemas/federated", d)
	assert.Nil(t, err)

	d.Body = []byte("{\"test\":\"valid_json\"}")
	err = validator("error", "file://../../schemas/federated", d)
	assert.Error(t, err, "validator did not fail when it should")
	assert.True(t, strings.Contains(err.Error(), "original-message is required"))

	d.Body = d.Body[:20]
	err = validator("error", "file://../../schemas/federated", d)
	assert.Error(t, err, "validator did not fail when it should")

	err = validator("ready", "file://../../schemas/federated", d)
	assert.Error(t, err, "validator did not fail when it should")

	d.Body = []byte("{\"test\":\"valid_json\"}")
	err = validator("ready", "file://../../schemas/federated", d)
	assert.Error(t, err, "validator did not fail when it should")
	assert.True(t, strings.Contains(err.Error(), "user is required"))

	finalizedMsg := common.Finalize{
		User:        "JohnDoe",
		Filepath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []common.Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	d.Body, _ = json.Marshal(finalizedMsg)
	err = validator("ready", "file://../../schemas/federated", d)
	assert.Nil(t, err)
}

func TestSendEmail(t *testing.T) {
	server := smtpmock.New(smtpmock.ConfigurationAttr{
		LogToStdout:       true,
		LogServerActivity: true,
	})

	if err := server.Start(); err != nil {
		fmt.Println(err)
	}

	hostAddress, portNumber := "127.0.0.1", server.PortNumber

	conf := config.SMTPConf{
		Password: "",
		FromAddr: "noreploy@testing",
		Host:     hostAddress,
		Port:     portNumber,
	}

	err := sendEmail(conf, "Mail Body", "recipient", "subject")
	assert.Equal(t, "smtp: server doesn't support AUTH", err.Error())
}
