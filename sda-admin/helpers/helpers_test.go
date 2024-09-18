package helpers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBody(t *testing.T) {
	// Mock server
	mockResponse := `{"key": "value"}`
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, err := rw.Write([]byte(mockResponse))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Test successful case
	body, err := GetBody(server.URL, "mock_token")
	assert.NoError(t, err)
	assert.JSONEq(t, mockResponse, string(body))

	// Test non-200 status code
	serverError := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
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

		_, err := rw.Write([]byte(mockResponse))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Test successful case
	body, err := PostReq(server.URL, "mock_token", []byte(`{"name":"test"}`))
	assert.NoError(t, err)
	assert.JSONEq(t, mockResponse, string(body))

	// Test non-200 status code
	serverError := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
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
