package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// CegaUserResponse captures the response list
type CegaUserResponse struct {
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
func authenticateWithCEGA(conf config.CegaConfig, username string) (*http.Response, error) {
	client := &http.Client{}
	payload := strings.NewReader("")
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", strings.TrimSuffix(conf.AuthURL, "/"), username), payload)

	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Basic "+getb64Credentials(conf.ID, conf.Secret))
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)

	return res, err
}
