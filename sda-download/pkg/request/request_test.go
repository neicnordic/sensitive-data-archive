package request

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

// Mock client code below from https://hassansin.github.io/Unit-Testing-http-client-in-Go

// RoundTripFunc
type RoundTripFunc func(req *http.Request) *http.Response

// RoundTrip
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

// NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func newTestClient(fn RoundTripFunc) *http.Client {
	return &http.Client{
		Transport: RoundTripFunc(fn),
	}
}

func TestInitialiseClient(t *testing.T) {
	// Initialise HTTP client
	client, err := InitialiseClient()
	if err != nil {
		t.Fatalf("http client creation failed %s", err)
	}

	// Verify that the correct type of object was created
	if reflect.TypeOf(client).String() != "*http.Client" {
		t.Errorf("http client creation failed, wanted *http.Client, received %s", reflect.TypeOf(client))
	}
}

func TestMakeRequest_Fail_HTTPNewRequest(t *testing.T) {

	// Save original to-be-mocked functions
	originalHTTPMakeRequest := HTTPNewRequest

	// Substitute mock functions
	HTTPNewRequest = func(_, _ string, _ io.Reader) (*http.Request, error) {
		return nil, errors.New("failed to build http request")
	}

	// Run test
	response, err := MakeRequest("GET", "https://testing.fi", nil, nil)
	// defer response.Body.Close()

	// Expected results
	expectedError := "failed to build http request"

	if response != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		defer response.Body.Close()
		t.Error("TestMakeRequest_Fail_HTTPNewRequest failed, expected nil")
	}
	if err.Error() != expectedError {
		t.Errorf("TestMakeRequest_Fail_HTTPNewRequest failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	HTTPNewRequest = originalHTTPMakeRequest

}

func TestMakeRequest_Fail_StatusCode(t *testing.T) {

	// Create mock client
	client := newTestClient(func(_ *http.Request) *http.Response {
		return &http.Response{
			StatusCode: 500,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(`error`)),
			// Response headers
			Header: make(http.Header),
		}
	})
	Client = client

	// Save original to-be-mocked functions
	originalHTTPMakeRequest := HTTPNewRequest

	// Substitute mock functions
	HTTPNewRequest = func(_, _ string, _ io.Reader) (*http.Request, error) {
		u, _ := url.ParseRequestURI("https://testing.fi")
		r := &http.Request{
			Method: "GET",
			URL:    u,
		}

		return r, nil
	}

	// Run test
	response, err := MakeRequest("GET", "https://testing.fi", nil, nil)

	// Expected results
	expectedError := "500"

	if response != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		defer response.Body.Close()
		t.Error("TestMakeRequest_Fail_StatusCode failed, expected nil")
	}
	if err.Error() != expectedError {
		t.Errorf("TestMakeRequest_Fail_StatusCode failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	HTTPNewRequest = originalHTTPMakeRequest

}

func TestMakeRequest_Success(t *testing.T) {

	// Create mock client
	client := newTestClient(func(_ *http.Request) *http.Response {
		return &http.Response{
			StatusCode: 200,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(`hello`)),
			// Response headers
			Header: make(http.Header),
		}
	})
	Client = client

	// Save original to-be-mocked functions
	originalHTTPMakeRequest := HTTPNewRequest

	// Substitute mock functions
	HTTPNewRequest = func(_, _ string, _ io.Reader) (*http.Request, error) {
		u, _ := url.ParseRequestURI("https://testing.fi")
		r := &http.Request{
			Method: "GET",
			URL:    u,
		}

		return r, nil
	}

	// Run test
	response, err := MakeRequest("GET", "https://testing.fi", nil, nil)
	body, _ := io.ReadAll(response.Body)
	defer response.Body.Close()

	// Expected results
	expectedBody := "hello"

	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestMakeRequest_Success failed, got %s expected %s", string(body), string(expectedBody))
	}
	if err != nil {
		t.Errorf("TestMakeRequest_Success failed, expected nil received %v", err)
	}

	// Return mock functions to originals
	HTTPNewRequest = originalHTTPMakeRequest

}
