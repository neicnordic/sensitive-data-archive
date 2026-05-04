package file

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/tidwall/pretty"
	"golang.org/x/term"
)

// ErrAborted is returned when the user explicitly cancels an interactive prompt (e.g. Ctrl+C).
var ErrAborted = errors.New("aborted by user")

type RequestBodyFileIngest struct {
	Filepath string `json:"filepath"`
	User     string `json:"user"`
}

type RequestBodyFileAccession struct {
	AccessionID string `json:"accession_id"`
	Filepath    string `json:"filepath"`
	User        string `json:"user"`
}

// List fetches and prints all files for username, auto-paginating.
// After each page (when more remain) the user is prompted to press Enter or
// Space for the next page, or Ctrl+C to abort.
func List(apiURI, token, username string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "users", username, "files")

	cursor := ""
	for {
		u := *parsedURL
		if cursor != "" {
			q := u.Query()
			q.Set("cursor", cursor)
			u.RawQuery = q.Encode()
		}

		body, headers, err := helpers.GetPagedResponseBody(u.String(), token)
		if err != nil {
			return err
		}

		fmt.Print(string(pretty.Pretty(body)))

		cursor = headers.Get("X-Next-Cursor")
		if cursor == "" {
			break
		}

		fmt.Fprint(os.Stderr, "-- Press [Enter] or [Space] for next page, Ctrl+C to quit --")
		if err := waitForContinue(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr)
	}

	return nil
}

// waitForContinue is a variable so it can be replaced in tests.
var waitForContinue = waitForUserContinue

// waitForUserContinue waits for the user to press Enter or Space before showing
// the next page. In raw terminal mode a single keystroke suffices; when stdin
// is not a tty (e.g. piped input or tests) it auto-continues.
func waitForUserContinue() error {
	fd := int(os.Stdin.Fd()) //nolint:gosec
	if !term.IsTerminal(fd) {
		return nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		_, scanErr := fmt.Fscanln(os.Stdin)
		if scanErr == nil || errors.Is(scanErr, io.EOF) {
			return nil
		}

		return scanErr
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return err
		}
		switch buf[0] {
		case '\r', '\n', ' ':
			return nil
		case 3: // Ctrl+C
			fmt.Fprintln(os.Stderr)

			return ErrAborted
		}
	}
}

// Ingest triggers the ingestion of a file via the SDA API.
// Depending on the provided fields in ingestInfo:
// - If ingestInfo.Id is empty, it sends a POST request to /file/ingest with a JSON body containing the file path and user.
// - If ingestInfo.Id is set, it sends a POST request to /file/ingest with the fileid as a query parameter and no JSON body.
func Ingest(ingestInfo helpers.FileInfo) error {
	var jsonBody []byte
	parsedURL, err := url.Parse(ingestInfo.URL)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "file/ingest")

	if ingestInfo.ID == "" {
		if err := helpers.CheckValidChars(ingestInfo.Path); err != nil {
			return err
		}
		requestBody := RequestBodyFileIngest{
			Filepath: ingestInfo.Path,
			User:     ingestInfo.User,
		}
		jsonBody, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON, reason: %v", err)
		}
	} else {
		query := parsedURL.Query()
		query.Set("fileid", ingestInfo.ID)
		parsedURL.RawQuery = query.Encode()
		jsonBody = nil
	}

	_, err = helpers.PostRequest(parsedURL.String(), ingestInfo.Token, jsonBody)
	if err != nil {
		return err
	}

	return nil
}

// SetAccession assigns an accession ID to a file via the SDA API.
// Depending on the provided fields in accessionInfo:
// - If accessionInfo.Id is empty, it sends a POST request to /file/accession with a JSON body containing accession_id, filepath, and user.
// - If accessionInfo.Id is set, it sends a POST request to /file/accession with fileid and accessionid as query parameters.
func SetAccession(accessionInfo helpers.FileInfo) error {
	var jsonBody []byte
	parsedURL, err := url.Parse(accessionInfo.URL)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "file/accession")

	if accessionInfo.ID == "" {
		if err := helpers.CheckValidChars(accessionInfo.Path); err != nil {
			return err
		}
		requestBody := RequestBodyFileAccession{
			AccessionID: accessionInfo.Accession,
			Filepath:    accessionInfo.Path,
			User:        accessionInfo.User,
		}
		jsonBody, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON, reason: %v", err)
		}
	} else {
		query := parsedURL.Query()
		query.Set("fileid", accessionInfo.ID)
		query.Set("accessionid", accessionInfo.Accession)
		parsedURL.RawQuery = query.Encode()
		jsonBody = nil
	}

	_, err = helpers.PostRequest(parsedURL.String(), accessionInfo.Token, jsonBody)
	if err != nil {
		return err
	}

	return nil
}

// RotateKey rotates the encryption key for a file
func RotateKey(apiURI, token, fileID string) error {
	parsedURL, err := url.Parse(apiURI)
	if err != nil {
		return err
	}
	parsedURL.Path = path.Join(parsedURL.Path, "file", "rotatekey", fileID)

	_, err = helpers.PostRequest(parsedURL.String(), token, nil)
	if err != nil {
		return fmt.Errorf("failed to rotate key for file %s, reason: %v", fileID, err)
	}

	return nil
}
