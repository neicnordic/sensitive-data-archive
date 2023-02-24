package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func TestValidateJSON(t *testing.T) {
	okMsg := Finalize{
		User:        "JohnDoe",
		Filepath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
			{Type: "md5", Value: "68b329da9893e34099c7d8ad5cb9c940"},
		},
	}

	msg, _ := json.Marshal(okMsg)
	res, err := ValidateJSON("file://../../schemas/federated/ingestion-completion.json", msg)
	assert.Nil(t, err)
	assert.True(t, res.Valid())

	badMsg := Finalize{
		User:        "JohnDoe",
		Filepath:    "path/to file",
		AccessionID: "EGAF00123456789",
		DecryptedChecksums: []Checksums{
			{Type: "sha256", Value: "da886a89637d125ef9f15f6d676357f3a9e5e10306929f0bad246375af89c2e2"},
		},
	}

	msg, _ = json.Marshal(badMsg)
	res, err = ValidateJSON("file://../../schemas/federated/ingestion-completion.json", msg)
	assert.Nil(t, err)
	assert.False(t, res.Valid())

}
