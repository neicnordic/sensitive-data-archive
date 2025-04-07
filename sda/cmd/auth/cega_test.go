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

func (ct *CegaTests) TestGetb64Credentials() {
	user := "testUser"
	password := "password"

	expected := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))

	assert.Equal(ct.T(), expected, getb64Credentials(user, password), "base64 encoding of credentials failing")
}

func (ct *CegaTests) TestVerifyPassword() {
	password := "password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error(err)
	}

	assert.Equal(ct.T(), true, verifyPassword(password, string(hash)), "password hash verification failing on correct hash")
	assert.Equal(ct.T(), false, verifyPassword(password, "wronghash"), "password hash verification returning true for wrong hash")
}
