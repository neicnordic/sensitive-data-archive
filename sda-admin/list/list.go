package list

import (
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

// ListUsers returns all users
func ListUsers(apiURI, token string) error {

	url := apiURI + "/users"
	response, err := helpers.GetResponseBody(url, token)
	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}

// ListFiles returns all files
func ListFiles(apiURI, token, username string) error {
	response, err := helpers.GetResponseBody(fmt.Sprintf("%s/users/%s/files", apiURI, username), token)

	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}
