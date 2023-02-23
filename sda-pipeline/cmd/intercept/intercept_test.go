package main

import (
	"encoding/json"
	"testing"

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

type accession struct {
	Type               string      `json:"type"`
	User               string      `json:"user"`
	FilePath           string      `json:"filepath"`
	AccessionID        string      `json:"accession_id"`
	DecryptedChecksums []Checksums `json:"decrypted_checksums"`
}

type Checksums struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ingest struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	FilePath string `json:"filepath"`
}

type mapping struct {
	Type        string   `json:"type"`
	DatasetID   string   `json:"dataset_id"`
	AcessionIDs []string `json:"accession_ids"`
}

type missing struct {
	User     string `json:"user"`
	FilePath string `json:"filepath"`
}

func (suite *TestSuite) TestMessageSelection_Accession() {
	msg := accession{
		Type:        "accession",
		User:        "foo",
		FilePath:    "/tmp/foo",
		AccessionID: "EGAF12345678901",
		DecryptedChecksums: []Checksums{
			{"md5", "7Ac236b1a8dce2dac89e7cf45d2b48BD"},
		},
	}
	message, _ := json.Marshal(&msg)

	msgType, err := typeFromMessage(message)

	assert.Nil(suite.T(), err, "Unexpected error from typeFromMessage")
	assert.Equal(suite.T(), msgType, msgAccession, "message type from message does not match expected")

	schema, err := schemaNameFromType(msgType)
	assert.Equal(suite.T(), schema, "ingestion-accession")
	assert.Nil(suite.T(), err, "Unexpected error from schemaNameFromType")
}

func (suite *TestSuite) TestMessageSelection_Cancel() {
	msg := ingest{
		Type:     "cancel",
		User:     "foo",
		FilePath: "/tmp/foo",
	}
	message, _ := json.Marshal(&msg)

	msgType, err := typeFromMessage(message)

	assert.Nil(suite.T(), err, "Unexpected error from typeFromMessage")
	assert.Equal(suite.T(), msgType, msgCancel, "message type from message does not match expected")

	schema, err := schemaNameFromType(msgType)
	assert.Equal(suite.T(), schema, "ingestion-trigger")
	assert.Nil(suite.T(), err, "Unexpected error from schemaNameFromType")
}

func (suite *TestSuite) TestMessageSelection_Ingest() {
	msg := ingest{
		Type:     "ingest",
		User:     "foo",
		FilePath: "/tmp/foo",
	}
	message, _ := json.Marshal(&msg)

	msgType, err := typeFromMessage(message)

	assert.Nil(suite.T(), err, "Unexpected error from typeFromMessage")
	assert.Equal(suite.T(), msgIngest, msgType, "message type from message does not match expected")

	schema, err := schemaNameFromType(msgType)
	assert.Equal(suite.T(), schema, "ingestion-trigger")
	assert.Nil(suite.T(), err, "Unexpected error from schemaNameFromType")

}

func (suite *TestSuite) TestMessageSelection_Mapping() {
	msg := mapping{
		Type:      "mapping",
		DatasetID: "EGAD12345678900",
		AcessionIDs: []string{
			"EGAF12345678901",
		},
	}
	message, _ := json.Marshal(&msg)

	msgType, err := typeFromMessage(message)

	assert.Nil(suite.T(), err, "Unexpected error from typeFromMessage")
	assert.Equal(suite.T(), msgMapping, msgType, "message type from message does not match expected")

	schema, err := schemaNameFromType(msgType)
	assert.Equal(suite.T(), schema, "dataset-mapping")
	assert.Nil(suite.T(), err, "Unexpected error from schemaNameFromType")
}

func (suite *TestSuite) TestMessageSelection_Notype() {
	msg := missing{
		User:     "foo",
		FilePath: "/tmp/foo",
	}
	message, _ := json.Marshal(&msg)

	msgType, err := typeFromMessage(message)

	assert.Error(suite.T(), err, "Unexpected lack of error from typeFromMessage")
	assert.Equal(suite.T(), "", msgType, "message type from message does not match expected")

	_, err = schemaNameFromType(msgType)
	assert.Error(suite.T(), err, "schemaNameFromType did not fail as expected")

}
