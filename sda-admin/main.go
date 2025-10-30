package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/c4ghkeyhash"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/dataset"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/file"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/user"
)

var version = "development"

var (
	apiURI string
	token  string
)

// Command-line usage
const usage = `usage: sda-admin [-uri URI] [-token TOKEN] <command> [options]

Commands:
  user list                     List all users.
  file list -user USERNAME      List all files for a specified user.
  file ingest -filepath FILEPATH -user USERNAME
                                Trigger ingestion of a given file.
  file set-accession -filepath FILEPATH -user USERNAME -accession-id accessionID
                                Assign accession ID to a file.
  dataset create -user SUBMISSION_USER -dataset-id DATASET_ID accessionID [accessionID ...]
                                Create a dataset from a list of accession IDs and a dataset ID.
  dataset release -dataset-id DATASET_ID
                                Release a dataset for downloading.
  
Global Options:
  -uri URI         Set the URI for the API server (optional if API_HOST is set).
  -token TOKEN     Set the authentication token (optional if ACCESS_TOKEN is set).

Additional Commands:
  version          Show the version of sda-admin.
  help             Show this help message.
  -h, -help        Show this help message`

var userUsage = `List Users:
  Usage: sda-admin user list 
    List all users in the system with ongoing submissions.`

var userListUsage = `Usage: sda-admin user list 
  List all users in the system with ongoing submissions.`

var fileUsage = `List all files for a user:
  Usage: sda-admin file list -user USERNAME
	List all files for a specified user.

Ingest a file:
  Usage: sda-admin file ingest -filepath FILEPATH -user USERNAME
    Trigger the ingestion of a given file for a specific user.

Set accession ID to a file:
  Usage: sda-admin file set-accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
    Assign an accession ID to a file for a given user.

Options:
  -user USERNAME       Specify the username associated with the file.
  -filepath FILEPATH   Specify the path of the file to ingest.
  -accession-id ID     Specify the accession ID to assign to the file.

Use 'sda-admin help file <command>' for information on a specific command.`

var fileListUsage = `Usage: sda-admin file list -user USERNAME
  List all files for a specified user.

Options:
  -user USERNAME 	Specify the username associated with the files.`

var fileIngestUsage = `Usage with file path and user: sda-admin file ingest -filepath FILEPATH -user USERNAME
Usage with file ID: sda-admin file ingest -fileid FILEUUID

  Trigger the ingestion either by providing filepath and user or file ID.

Options:
  -filepath FILEPATH   Specify the path of the file to ingest.
  -user USERNAME       Specify the username associated with the file.
  -fileid FILEUUID     Specify the file ID (UUID) of the file to ingest.`

var fileAccessionUsage = `Usage with file path and user: sda-admin file set-accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
Usage with file ID: sda-admin file set-accession -fileid FILEUUID -accession-id ACCESSION_ID

  Assign accession ID to a file by providing filepath and user or file ID.

Options:
  -filepath FILEPATH   Specify the path of the file to assign the accession ID.
  -user USERNAME       Specify the username associated with the file.
  -fileid FILEUUID     Specify the file ID of the file to assign the accession ID.
  -accession-id ID     Specify the accession ID to assign to the file.`

var datasetUsage = `Create a dataset:
  Usage: sda-admin dataset create -user SUBMISSION_USER -dataset-id DATASET_ID [ACCESSION_ID ...]
    Create a dataset from a list of accession IDs and a dataset ID.
    
Release a dataset:
  Usage: sda-admin dataset release -dataset-id DATASET_ID
    Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID   Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]       (For dataset create) Specify one or more accession IDs to include in the dataset.

Use 'sda-admin help dataset <command>' for information on a specific command.`

var datasetCreateUsage = `Usage: sda-admin dataset create -user SUBMISSION_USER -dataset-id DATASET_ID [ACCESSION_ID ...]
  Create a dataset from a list of accession IDs and a dataset ID belonging to a given user.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]         (For dataset create) Specify one or more accession IDs to include in the dataset.`

var datasetReleaseUsage = `Usage: sda-admin dataset release -dataset-id DATASET_ID
  Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.`

var c4ghHashUsage = `Handles the crypt4gh keys in the system.

Usage: sda-admin c4gh-hash add -filepath FILEPATH -description DESCRIPTION
Registers a new key hash.

Options:
  -filepath FILEPATH       Specify the path of the public key to register in the key hash table.
  -description DESCRIPTION Description for the Crypt4gh key.

Usage: sda-admin c4gh-hash deprecate -hash KEYHASH
Deprecates a keyhash

Options:
  -hash KEYHASH The keyhash that should be deprecated

Usage: sda-admin c4gh-hash list
Lists all key hashes in the system`

var c4ghHashAddUsage = `Usage: sda-admin c4gh-hash add -filepath FILEPATH -description DESCRIPTION
Registers a new key hash.

Options:
  -filepath FILEPATH       Specify the path of the public key to register in the key hash table.
  -description DESCRIPTION Description for the Crypt4gh key.`

var c4ghHashDeprecateUsage = `Usage: sda-admin c4gh-hash deprecate -hash KEYHASH
Deprecates a keyhash

Options:
  -hash KEYHASH The keyhash that should be deprecated`

var c4ghHashListUsage = `Usage: sda-admin c4gh-hash list
Lists all key hashes in the system.`

func printVersion() {
	fmt.Printf("sda-admin %s\n", version)
}

func parseFlagsAndEnv() error {
	// Set up flags
	flag.StringVar(&apiURI, "uri", "", "Set the URI for the SDA server (optional if API_HOST is set)")
	flag.StringVar(&token, "token", "", "Set the authentication token (optional if ACCESS_TOKEN is set)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Println(usage)
	}

	// Parse global flags first
	flag.Parse()

	// If no command is provided, show usage
	if flag.NArg() == 0 {
		return errors.New(usage)
	}

	if flag.Arg(0) == "help" || flag.Arg(0) == "version" {
		return nil
	}

	// Check environment variables if flags are not provided
	if apiURI == "" {
		apiURI = os.Getenv("API_HOST")
		if apiURI == "" {
			return errors.New("error: either -uri must be provided or API_HOST environment variable must be set")
		}
	}

	if token == "" {
		token = os.Getenv("ACCESS_TOKEN")
		if token == "" {
			return errors.New("error: either -token must be provided or ACCESS_TOKEN environment variable must be set")
		}
	}

	return nil
}

func handleHelpCommand() error {
	if flag.NArg() == 1 {
		fmt.Println(usage)

		return nil
	}

	switch flag.Arg(1) {
	case "user":
		if err := handleHelpUser(); err != nil {
			return err
		}
	case "file":
		if err := handleHelpFile(); err != nil {
			return err
		}
	case "dataset":
		if err := handleHelpDataset(); err != nil {
			return err
		}
	case "c4gh-hash":
		if err := handleHelpC4ghKeyHash(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command '%s'.\n%s", flag.Arg(1), usage)
	}

	return nil
}

func handleHelpUser() error {
	switch {
	case flag.NArg() == 2:
		fmt.Println(userUsage)
	case flag.Arg(2) == "list":
		fmt.Println(userListUsage)
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(2), flag.Arg(1), userUsage)
	}

	return nil
}

func handleHelpFile() error {
	switch {
	case flag.NArg() == 2:
		fmt.Println(fileUsage)
	case flag.Arg(2) == "list":
		fmt.Println(fileListUsage)
	case flag.Arg(2) == "ingest":
		fmt.Println(fileIngestUsage)
	case flag.Arg(2) == "set-accession":
		fmt.Println(fileAccessionUsage)
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(2), flag.Arg(1), fileUsage)
	}

	return nil
}

func handleHelpDataset() error {
	switch {
	case flag.NArg() == 2:
		fmt.Println(datasetUsage)
	case flag.Arg(2) == "create":
		fmt.Println(datasetCreateUsage)
	case flag.Arg(2) == "release":
		fmt.Println(datasetReleaseUsage)
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(2), flag.Arg(1), datasetUsage)
	}

	return nil
}

func handleUserCommand() error {
	if flag.NArg() < 2 {
		return fmt.Errorf("error: 'user' requires a subcommand (list).\n%s", userUsage)
	}
	switch flag.Arg(1) {
	case "list":
		err := user.List(apiURI, token)
		if err != nil {
			return fmt.Errorf("error: failed to get users, reason: %v", err)
		}
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(1), flag.Arg(0), userUsage)
	}

	return nil
}

func handleFileListCommand() error {
	listFilesCmd := flag.NewFlagSet("list", flag.ExitOnError)
	var username string
	listFilesCmd.StringVar(&username, "user", "", "Filter files by username")

	if err := listFilesCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	// Check if the -user flag was provided
	if username == "" {
		return fmt.Errorf("error: the -user flag is required.\n%s", fileListUsage)
	}

	if err := file.List(apiURI, token, username); err != nil {
		return fmt.Errorf("error: failed to get files, reason: %v", err)
	}

	return nil
}

func handleFileCommand() error {
	if flag.NArg() < 2 {
		return fmt.Errorf("error: 'file' requires a subcommand (list, ingest, set-accession).\n%s", fileUsage)
	}
	switch flag.Arg(1) {
	case "list":
		if err := handleFileListCommand(); err != nil {
			return err
		}
	case "ingest":
		if err := handleFileIngestCommand(); err != nil {
			return err
		}
	case "set-accession":
		if err := handleFileAccessionCommand(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(1), flag.Arg(0), fileUsage)
	}

	return nil
}

func handleFileIngestCommand() error {
	fileIngestCmd := flag.NewFlagSet("ingest", flag.ExitOnError)
	var ingestInfo helpers.FileInfo
	ingestInfo.URL = apiURI
	ingestInfo.Token = token
	fileIngestCmd.StringVar(&ingestInfo.Path, "filepath", "", "Filepath to ingest")
	fileIngestCmd.StringVar(&ingestInfo.User, "user", "", "Username to associate with the file")
	fileIngestCmd.StringVar(&ingestInfo.ID, "fileid", "", "File ID (UUID) to ingest")

	if err := fileIngestCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	switch {
	case ingestInfo.Path == "" && ingestInfo.User == "" && ingestInfo.ID == "":
		return fmt.Errorf("error: either -filepath and -user pair or -fileid are required.\n%s", fileIngestUsage)
	case ingestInfo.ID != "" && (ingestInfo.Path != "" || ingestInfo.User != ""):
		return fmt.Errorf("error: choose if -filepath and -user pair or -fileid will be used.\n%s", fileIngestUsage)
	case ingestInfo.ID == "" && (ingestInfo.Path == "" || ingestInfo.User == ""):
		return fmt.Errorf("error: both -filepath and -user must be provided together.\n%s", fileIngestUsage)
	default:
		err := file.Ingest(ingestInfo)
		if err != nil {
			return fmt.Errorf("error: failed to ingest file, reason: %v", err)
		}

		return nil
	}
}

func handleFileAccessionCommand() error {
	fileAccessionCmd := flag.NewFlagSet("set-accession", flag.ExitOnError)
	var accessionInfo helpers.FileInfo
	accessionInfo.URL = apiURI
	accessionInfo.Token = token
	fileAccessionCmd.StringVar(&accessionInfo.Path, "filepath", "", "Filepath to assign accession ID")
	fileAccessionCmd.StringVar(&accessionInfo.User, "user", "", "Username to associate with the file")
	fileAccessionCmd.StringVar(&accessionInfo.Accession, "accession-id", "", "Accession ID to assign")
	fileAccessionCmd.StringVar(&accessionInfo.ID, "fileid", "", "File ID (UUID) to ingest")

	if err := fileAccessionCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	switch {
	case accessionInfo.ID == "" && accessionInfo.Path == "" && accessionInfo.User == "" && accessionInfo.Accession == "":
		return fmt.Errorf("error: no arguments provided.\n%s", fileAccessionUsage)
	case accessionInfo.ID == "" && (accessionInfo.Path == "" || accessionInfo.User == "" || accessionInfo.Accession == ""):
		return fmt.Errorf("error: -filepath, -user, and -accession-id are required.\n%s", fileAccessionUsage)
	case accessionInfo.ID != "" && accessionInfo.Accession != "" && (accessionInfo.Path != "" || accessionInfo.User != ""):
		return fmt.Errorf("error: when using -fileid, do not provide -filepath or -user together. Only -fileid and -accession-id are allowed.\n%s", fileAccessionUsage)
	case accessionInfo.ID != "" && accessionInfo.Accession == "" && (accessionInfo.Path == "" && accessionInfo.User == ""):
		return fmt.Errorf("error: -accession-id is required.\n%s", fileAccessionUsage)
	case accessionInfo.ID == "" && accessionInfo.Path != "" && accessionInfo.User != "" && accessionInfo.Accession == "":
		return fmt.Errorf("error: -accession-id is required.\n%s", fileAccessionUsage)
	default:
		err := file.SetAccession(accessionInfo)
		if err != nil {
			return fmt.Errorf("error: failed to assign accession ID to file, reason: %v", err)
		}

		return nil
	}
}

func handleDatasetCommand() error {
	if flag.NArg() < 2 {
		return fmt.Errorf("error: 'dataset' requires a subcommand (create, release).\n%s", datasetUsage)
	}

	switch flag.Arg(1) {
	case "create":
		if err := handleDatasetCreateCommand(); err != nil {
			return err
		}
	case "release":
		if err := handleDatasetReleaseCommand(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(1), flag.Arg(0), datasetUsage)
	}

	return nil
}

func handleDatasetCreateCommand() error {
	datasetCreateCmd := flag.NewFlagSet("create", flag.ExitOnError)
	var datasetID, username string
	datasetCreateCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to create")
	datasetCreateCmd.StringVar(&username, "user", "", "Username to associate with the file")

	if err := datasetCreateCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	accessionIDs := datasetCreateCmd.Args() // Args() returns the non-flag arguments after parsing

	if datasetID == "" || len(accessionIDs) == 0 {
		return fmt.Errorf("error: -dataset-id and at least one accession ID are required.\n%s", datasetCreateUsage)
	}

	if username == "" {
		return fmt.Errorf("error: -user is required.\n%s", datasetCreateUsage)
	}

	err := dataset.Create(apiURI, token, datasetID, username, accessionIDs)
	if err != nil {
		return fmt.Errorf("error: failed to create dataset, reason: %v", err)
	}

	return nil
}

func handleDatasetReleaseCommand() error {
	datasetReleaseCmd := flag.NewFlagSet("release", flag.ExitOnError)
	var datasetID string
	datasetReleaseCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to release")

	if err := datasetReleaseCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	if datasetID == "" {
		return fmt.Errorf("error: -dataset-id is required.\n%s", datasetReleaseUsage)
	}

	err := dataset.Release(apiURI, token, datasetID)
	if err != nil {
		return fmt.Errorf("error: failed to release dataset, reason: %v", err)
	}

	return nil
}

func handleHelpC4ghKeyHash() error {
	switch {
	case flag.NArg() == 2:
		fmt.Println(c4ghHashUsage)
	case flag.Arg(2) == "add":
		fmt.Println(c4ghHashAddUsage)
	case flag.Arg(2) == "deprecate":
		fmt.Println(c4ghHashDeprecateUsage)
	case flag.Arg(2) == "list":
		fmt.Println(c4ghHashListUsage)
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(2), flag.Arg(1), c4ghHashUsage)
	}

	return nil
}
func handleC4ghKeyHashCommand() error {
	if flag.NArg() < 2 {
		return fmt.Errorf("error: 'c4gh-hash' requires a subcommand (add, deprecate or list).\n%s", c4ghHashUsage)
	}

	switch flag.Arg(1) {
	case "add":
		if err := handleC4ghKeyHashAddCommand(); err != nil {
			return err
		}
	case "deprecate":
		if err := handleC4ghKeyHashDeprecateCommand(); err != nil {
			return err
		}
	case "list":
		if err := handleC4ghHashListCommand(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown subcommand '%s' for '%s'.\n%s", flag.Arg(1), flag.Arg(0), c4ghHashUsage)
	}

	return nil
}

func handleC4ghKeyHashAddCommand() error {
	c4ghAddCmd := flag.NewFlagSet("add", flag.ExitOnError)
	var description, filepath string
	c4ghAddCmd.StringVar(&description, "description", "", "")
	c4ghAddCmd.StringVar(&filepath, "filepath", "", "Filepath to cr4pt4gh public key")

	if err := c4ghAddCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	if filepath == "" {
		return fmt.Errorf("error: -filepath is required.\n%s", c4ghHashAddUsage)
	}

	err := c4ghkeyhash.Add(apiURI, token, filepath, description)
	if err != nil {
		return fmt.Errorf("error: failed to release dataset, reason: %v", err)
	}

	return nil
}

func handleC4ghKeyHashDeprecateCommand() error {
	c4ghDeprecateCmd := flag.NewFlagSet("deprecate", flag.ExitOnError)
	var hash string
	c4ghDeprecateCmd.StringVar(&hash, "hash", "", "hash of the key to deprecate")

	if err := c4ghDeprecateCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	if hash == "" {
		return fmt.Errorf("error: -hash string is required.\n%s", c4ghHashDeprecateUsage)
	}

	err := c4ghkeyhash.Deprecate(apiURI, token, hash)
	if err != nil {
		return fmt.Errorf("error: failed to release dataset, reason: %v", err)
	}

	return nil
}

func handleC4ghHashListCommand() error {
	err := c4ghkeyhash.List(apiURI, token)
	if err != nil {
		return fmt.Errorf("error: failed to list crypt4gh hashes, reason: %v", err)
	}

	return nil
}

func main() {
	if err := parseFlagsAndEnv(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "help", "-h", "-help":
		if err := handleHelpCommand(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "user":
		if err := handleUserCommand(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "file":
		if err := handleFileCommand(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "dataset":
		if err := handleDatasetCommand(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version":
		printVersion()
	case "c4gh-hash":
		if err := handleC4ghKeyHashCommand(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command '%s'.\n%s\n", flag.Arg(0), usage)
		os.Exit(1)
	}

	os.Exit(0)
}
