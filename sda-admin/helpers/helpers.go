package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
)

// Helper functions used by more than one module

// CheckTokenExpiration is used to determine whether the token is expiring in less than a day
func CheckTokenExpiration(accessToken string) error {
	// Parse jwt token with unverified, since we don't need to check the signatures here
	token, _, err := new(jwt.Parser).ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		return fmt.Errorf("could not parse token, reason: %s", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("broken token (claims are empty): %v\nerror: %s", claims, err)
	}

	// Check if the token has exp claim
	if claims["exp"] == nil {
		return fmt.Errorf("could not parse token, reason: no expiration date")
	}

	// Parse the expiration date from token, handle cases where
	//  the date format is nonstandard, e.g. test tokens are used
	var expiration time.Time
	switch iat := claims["exp"].(type) {
	case float64:
		expiration = time.Unix(int64(iat), 0)
	case json.Number:
		tmp, _ := iat.Int64()
		expiration = time.Unix(tmp, 0)
	case string:
		i, err := strconv.ParseInt(iat, 10, 64)
		if err != nil {
			return fmt.Errorf("could not parse token, reason: %s", err)
		}
		expiration = time.Unix(int64(i), 0)
	default:
		return fmt.Errorf("could not parse token, reason: unknown expiration date format")
	}

	switch untilExp := time.Until(expiration); {
	case untilExp < 0:
		return fmt.Errorf("the provided access token has expired, please renew it")
	case untilExp > 0 && untilExp < 24*time.Hour:
		fmt.Fprintln(
			os.Stderr,
			"The provided access token expires in",
			time.Until(expiration).Truncate(time.Second),
		)
		fmt.Fprintln(os.Stderr, "Consider renewing the token.")
	default:
		fmt.Fprintln(
			os.Stderr,
			"The provided access token expires in",
			time.Until(expiration).Truncate(time.Second),
		)
	}

	return nil
}

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

	// Check the status code
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", res.StatusCode)
	}

	// Read the response body
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, reason: %v", err)
	}

	defer res.Body.Close()

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

	// Check the status code
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", res.StatusCode)
	}

	// Read the response body
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, reason: %v", err)
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
