package helpers

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/stretchr/testify/assert"
)

// generate jwts for testing the expDate
func generateDummyToken(expDate int64) string {
	// Generate a new private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate private key: %s", err)
	}

	// Create the Claims
	claims := &jwt.StandardClaims{
		Issuer: "test",
	}
	if expDate != 0 {
		claims = &jwt.StandardClaims{
			ExpiresAt: expDate,
			Issuer:    "test",
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	ss, err := token.SignedString(privateKey)
	if err != nil {
		log.Fatalf("Failed to sign token: %s", err)
	}

	return ss
}

func TestTokenExpiration(t *testing.T) {
	// Token without exp claim
	token := generateDummyToken(0)
	err := CheckTokenExpiration(token)
	assert.EqualError(t, err, "could not parse token, reason: no expiration date")

	// Token with expired date
	token = generateDummyToken(time.Now().Unix())
	err = CheckTokenExpiration(token)
	assert.EqualError(t, err, "the provided access token has expired, please renew it")

	// Token with valid expiration
	token = generateDummyToken(time.Now().Add(time.Hour * 72).Unix())
	err = CheckTokenExpiration(token)
	assert.NoError(t, err)
}

func TestGetBody(t *testing.T) {
	// Mock server
	mockResponse := `{"key": "value"}`
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte(mockResponse))
	}))
	defer server.Close()

	// Test successful case
	body, err := GetBody(server.URL, "mock_token")
	assert.NoError(t, err)
	assert.JSONEq(t, mockResponse, string(body))

	// Test non-200 status code
	serverError := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer serverError.Close()

	body, err = GetBody(serverError.URL, "mock_token")
	assert.Error(t, err)
	assert.Nil(t, body)
}

func TestPostReq(t *testing.T) {
	// Mock server
	mockResponse := `{"key": "value"}` 
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer mock_token", req.Header.Get("Authorization"))
		rw.Write([]byte(mockResponse))
	}))
	defer server.Close()

	// Test successful case
	body, err := PostReq(server.URL, "mock_token", []byte(`{"name":"test"}`))
	assert.NoError(t, err)
	assert.JSONEq(t, mockResponse, string(body))

	// Test non-200 status code
	serverError := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer serverError.Close()

	body, err = PostReq(serverError.URL, "mock_token", []byte(`{"name":"test"}`))
	assert.Error(t, err)
	assert.Nil(t, body)
}

func TestInvalidCharacters(t *testing.T) {
	// Test that file paths with invalid characters trigger errors
	for _, badc := range "\x00\x7F\x1A:*?\\<>\"|!'();@&=+$,%#[]" {
		badchar := string(badc)
		testfilepath := "test" + badchar + "file"

		err := CheckValidChars(testfilepath)
		assert.Error(t, err)
		assert.Equal(t, fmt.Sprintf("filepath '%v' contains disallowed characters: %+v", testfilepath, badchar), err.Error())
	}
}
