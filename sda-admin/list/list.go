package list

import (
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
)

// ListUsers returns all users
func ListUsers(api_uri, token string) error {

	url := api_uri + "/users"
	response, err := helpers.GetResponseBody(url, token)
	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}

// ListFiles returns all files
func ListFiles(api_uri, token, username string) error {
	response, err := helpers.GetResponseBody(fmt.Sprintf("%s/users/%s/files", api_uri, username), token)

	if err != nil {
		return err
	}

	fmt.Println(string(response))

	return nil
}
