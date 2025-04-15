package main

import (
	"encoding/base64"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/bcrypt"
)

// These are not complete tests of all functions in elixir. New tests should
// be added as the code is updated.

type CegaTests struct {
	suite.Suite
}

func TestCegaTestSuite(t *testing.T) {
	suite.Run(t, new(CegaTests))
}

func (ts *CegaTests) TestGetb64Credentials() {
	user := "testUser"
	password := "password"

	expected := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))

	assert.Equal(ts.T(), expected, getb64Credentials(user, password), "base64 encoding of credentials failing")
}

func (ts *CegaTests) TestVerifyPassword() {
	password := "password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error(err)
	}

	assert.Equal(ts.T(), true, verifyPassword(password, string(hash)), "password hash verification failing on correct hash")
	assert.Equal(ts.T(), false, verifyPassword(password, "wronghash"), "password hash verification returning true for wrong hash")
}
