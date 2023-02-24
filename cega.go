package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
	bcrypt "golang.org/x/crypto/bcrypt"
)

// EGALoginError is used to store message errors
type EGALoginError struct {
	Reason string
}

// CegaUserResponse captures the response key
type CegaUserResponse struct {
	Results CegaUserResults `json:"response"`
}

// CegaUserResults captures the result key
type CegaUserResults struct {
	Response []CegaUserInfo `json:"result"`
}

// CegaUserInfo captures the password hash
type CegaUserInfo struct {
	PasswordHash string `json:"passwordHash"`
}

// EGAIdentity represents an EGA user instance
type EGAIdentity struct {
	User    string
	Token   string
	ExpDate string
}

// Return base64 encoded credentials for basic auth
func getb64Credentials(username, password string) string {
	creds := username + ":" + password

	return base64.StdEncoding.EncodeToString([]byte(creds))
}

// Check whether the returned hash corresponds to the given password
func verifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))

	return err == nil
}

// Authenticate against CEGA
func authenticateWithCEGA(conf CegaConfig, username string) (*http.Response, error) {
	client := &http.Client{}
	payload := strings.NewReader("")
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s?idType=username", conf.AuthURL, username), payload)

	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", "Basic "+getb64Credentials(conf.ID, conf.Secret))
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)

	return res, err
}
