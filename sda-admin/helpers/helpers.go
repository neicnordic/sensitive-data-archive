package helpers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// necessary for mocking in unit tests
var GetResponseBody = GetBody

// GetBody sends a GET request to the given URL and returns the body of the response
func GetBody(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// Add headers
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get response, reason: %v", err)
	}
	defer res.Body.Close()

	// Read the response body
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, reason: %v", err)
	}

	// Check the status code
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", res.StatusCode, string(resBody))
	}

	return resBody, nil
}

// necessary for mocking in unit tests
var PostRequest = PostReq

// PostReq sends a POST request to the server with a JSON body and returns the response body or an error.
func PostReq(url, token string, jsonBody []byte) ([]byte, error) {
	// Create a new POST request with the provided JSON body
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create the request, reason: %v", err)
	}

	// Add headers
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request, reason: %v", err)
	}
	defer res.Body.Close()

	// Read the response body
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, reason: %v", err)
	}

	// Check the status code
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", res.StatusCode, string(resBody))
	}

	return resBody, nil
}

// Check for invalid characters for filepath
func CheckValidChars(filename string) error {
	re := regexp.MustCompile(`[\\<>"\|\x00-\x1F\x7F\!\*\'\(\)\;\:\@\&\=\+\$\,\?\%\#\[\]]`)
	disallowedChars := re.FindAllString(filename, -1)
	if disallowedChars != nil {

		return fmt.Errorf(
			"filepath '%v' contains disallowed characters: %+v",
			filename,
			strings.Join(disallowedChars, ", "),
		)
	}

	return nil
}
