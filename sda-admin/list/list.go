package list

import (
	"fmt"
	"net/url"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

// Users returns all users
func Users(apiURI, token string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {

		return err
	}
	parsedURL.Path = fmt.Sprintf("%s/users", parsedURL.Path)


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
	parsedURL.Path = fmt.Sprintf("%s/users/%s/files", parsedURL.Path, username)

	response, err := helpers.GetResponseBody(parsedURL.String(), token)
	if err != nil {

		return err
	}

	fmt.Println(string(response))

	return nil
}
