package auth

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/pkg/request"
	"github.com/stretchr/testify/assert"
)

func TestGetOIDCDetails_Fail_MakeRequest(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		return nil, errors.New("error")
	}

	// Run test
	oidcDetails, err := GetOIDCDetails("https://testing.fi")

	// Expected results
	expectedUserInfo := ""
	expectedJWK := ""
	expectedError := "error"

	if oidcDetails.Userinfo != expectedUserInfo {
		t.Errorf("TestGetOIDCDetails_Fail_MakeRequest failed, expected %s, got %s", expectedUserInfo, oidcDetails.Userinfo)
	}
	if oidcDetails.JWK != expectedJWK {
		t.Errorf("TestGetOIDCDetails_Fail_MakeRequest failed, expected %s, got %s", expectedJWK, oidcDetails.JWK)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetOIDCDetails_Fail_MakeRequest failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest
}

func TestGetOIDCDetails_Fail_JSONDecode(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		response := &http.Response{
			StatusCode: 200,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(``)),
			// Response headers
			Header: make(http.Header),
		}

		return response, nil
	}

	// Run test
	oidcDetails, err := GetOIDCDetails("https://testing.fi")

	// Expected results
	expectedUserInfo := ""
	expectedJWK := ""
	expectedError := "EOF"

	if oidcDetails.Userinfo != expectedUserInfo {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected %s, got %s", expectedUserInfo, oidcDetails.Userinfo)
	}
	if oidcDetails.JWK != expectedJWK {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected %s, got %s", expectedJWK, oidcDetails.JWK)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest
}

func TestGetOIDCDetails_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		response := &http.Response{
			StatusCode: 200,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(`{"userinfo_endpoint":"https://aai.org/oidc/userinfo","jwks_uri":"https://aai.org/oidc/jwks"}`)),
			// Response headers
			Header: make(http.Header),
		}

		return response, nil
	}

	// Run test
	oidcDetails, err := GetOIDCDetails("https://testing.fi")

	// Expected results
	expectedUserInfo := "https://aai.org/oidc/userinfo"
	expectedJWK := "https://aai.org/oidc/jwks"

	if oidcDetails.Userinfo != expectedUserInfo {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected %s, got %s", expectedUserInfo, oidcDetails.Userinfo)
	}
	if oidcDetails.JWK != expectedJWK {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected %s, got %s", expectedJWK, oidcDetails.JWK)
	}
	if err != nil {
		t.Errorf("TestGetOIDCDetails_Fail_JSONDecode failed, expected nil received %v", err)
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest
}

func TestGetToken_Fail_EmptyHeader(t *testing.T) {

	// Test case
	header := http.Header{}
	header.Add("Authorization", "")
	token, code, err := GetToken(header)

	// Expected results
	expectedToken := ""
	expectedCode := 401
	expectedError := "access token must be provided"

	if token != expectedToken {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedToken, token)
	}
	if code != expectedCode {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %d, received %d", expectedCode, code)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedError, err.Error())
	}

}

func TestGetToken_X_Amz_Security_token(t *testing.T) {

	// Expected results
	expectedToken := "token"
	expectedCode := 0

	// Test case
	header := http.Header{}
	header.Add("X-Amz-Security-Token", expectedToken)
	token, code, err := GetToken(header)

	if token != expectedToken {
		t.Errorf("TestGetToken_X_Amz_Security_token failed, expected %s, received %s", expectedToken, token)
	}
	if code != expectedCode {
		t.Errorf("TestGetToken_X_Amz_Security_token failed, expected %d, received %d", expectedCode, code)
	}
	if err != nil {
		t.Errorf("TestGetToken_X_Amz_Security_token failed, expected nil, received %v", err)
	}

}

func TestGetToken_Fail_WrongScheme(t *testing.T) {

	// Test case
	header := http.Header{}
	header.Add("Authorization", "Basic token")
	token, code, err := GetToken(header)

	// Expected results
	expectedToken := ""
	expectedCode := 400
	expectedError := "authorization scheme must be bearer"

	if token != expectedToken {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedToken, token)
	}
	if code != expectedCode {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %d, received %d", expectedCode, code)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedError, err.Error())
	}

}

func TestGetToken_Fail_MissingToken(t *testing.T) {

	// Test case
	header := http.Header{}
	header.Add("Authorization", "Bearer")
	token, code, err := GetToken(header)

	// Expected results
	expectedToken := ""
	expectedCode := 400
	expectedError := "token string is missing from authorization header"

	if token != expectedToken {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedToken, token)
	}
	if code != expectedCode {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %d, received %d", expectedCode, code)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedError, err.Error())
	}

}

func TestGetToken_Success(t *testing.T) {

	// Test case
	header := http.Header{}
	header.Add("Authorization", "Bearer token")
	token, code, err := GetToken(header)

	// Expected results
	expectedToken := "token"
	expectedCode := 0

	if token != expectedToken {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %s, received %s", expectedToken, token)
	}
	if code != expectedCode {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected %d, received %d", expectedCode, code)
	}
	if err != nil {
		t.Errorf("TestGetToken_Fail_EmptyHeader failed, expected nil, received %v", err)
	}

}

func TestGetVisas_Fail_MakeRequest(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		return nil, errors.New("error")
	}

	// Run test
	oidcDetails := OIDCDetails{}
	visas, err := GetVisas(oidcDetails, "token")

	// Expected results
	expectedError := "error"

	if visas != nil {
		t.Errorf("TestGetVisas_Fail_MakeRequest failed, expected nil, got %v", visas)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetVisas_Fail_MakeRequest failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest

}

func TestGetVisas_Fail_JSONDecode(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		response := &http.Response{
			StatusCode: 200,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(``)),
			// Response headers
			Header: make(http.Header),
		}

		return response, nil
	}

	// Run test
	oidcDetails := OIDCDetails{}
	visas, err := GetVisas(oidcDetails, "token")

	// Expected results
	expectedError := "EOF"

	if visas != nil {
		t.Errorf("TestGetVisas_Fail_MakeRequest failed, expected nil, got %v", visas)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetVisas_Fail_MakeRequest failed, expected %s received %s", expectedError, err.Error())
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest

}

func TestGetVisas_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalMakeRequest := request.MakeRequest

	// Substitute mock functions
	request.MakeRequest = func(method, url string, headers map[string]string, body []byte) (*http.Response, error) {
		response := &http.Response{
			StatusCode: 200,
			// Response body
			Body: io.NopCloser(bytes.NewBufferString(`{"ga4gh_passport_v1":["visa1","visa2"]}`)),
			// Response headers
			Header: make(http.Header),
		}

		return response, nil
	}

	// Run test
	oidcDetails := OIDCDetails{}
	visas, err := GetVisas(oidcDetails, "token")

	// Expected results
	expectedVisas := []string{"visa1", "visa2"}

	if strings.Join(visas.Visa, "") != strings.Join(expectedVisas, "") {
		t.Errorf("TestGetVisas_Success failed, expected %v, got %v", expectedVisas, visas)
	}
	if err != nil {
		t.Errorf("TestGetVisas_Success failed, expected nil received %v", err)
	}

	// Return mock functions to originals
	request.MakeRequest = originalMakeRequest

}

func TestValidateTrustedIss(t *testing.T) {

	// this also tests checkIss
	trustedList := []config.TrustedISS([]config.TrustedISS{{ISS: "https://demo.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}, {ISS: "https://demo1.example", JKU: "https://mockauth:8000/idp/profile/oidc/keyset"}})

	ok := validateTrustedIss(trustedList, "https://demo.example", "https://mockauth:8000/idp/profile/oidc/keyset")

	assert.True(t, ok, "values might have changed in fixture")

	ok = validateTrustedIss(trustedList, "https://demo3.example", "https://mockauth:8000/idp/profile/oidc/keyset")

	assert.False(t, ok, "values might have changed in fixture")
}

func TestValidateTrustedIssNoConfig(t *testing.T) {

	ok := validateTrustedIss(nil, "https://demo.example", "https://mockauth:8000/idp/profile/oidc/keyset")

	assert.True(t, ok, "this should be true")
}
