package list

import (
	"fmt"
	"net/url"
	"path"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

// Users returns all users
func Users(apiURI, token string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "users")

	response, err := helpers.GetResponseBody(parsedURL.String(), token)
	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}

// Files returns all files
func Files(apiURI, token, username string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "users", username, "files")

	response, err := helpers.GetResponseBody(parsedURL.String(), token)
	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}
