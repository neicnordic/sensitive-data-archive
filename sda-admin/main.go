package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/dataset"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/file"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/user"
)

var version = "1.0.0"

var (
	apiURI string
	token  string
)

// Command-line usage
const usage = `Usage: sda-admin [-uri URI] [-token TOKEN] <command> [options]

Commands:
  user list                     List all users.
  file list -user USERNAME      List all files for a specified user.
  file ingest -filepath FILEPATH -user USERNAME
                                Trigger ingestion of a given file.
  file set-accession -filepath FILEPATH -user USERNAME -accession-id accessionID
                                Assign accession ID to a file.
  dataset create -dataset-id DATASET_ID accessionID [accessionID ...]
                                Create a dataset from a list of accession IDs and a dataset ID.
  dataset release -dataset-id DATASET_ID
                                Release a dataset for downloading.
  
Global Options:
  -uri URI         Set the URI for the API server (optional if API_HOST is set).
  -token TOKEN     Set the authentication token (optional if ACCESS_TOKEN is set).

Additional Commands:
  help             Show this help message.
  -h, -help        Show this help message.`

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

var fileIngestUsage = `Usage: sda-admin file ingest -filepath FILEPATH -user USERNAME
  Trigger the ingestion of a given file for a specific user.

Options:
  -filepath FILEPATH   Specify the path of the file to ingest.
  -user USERNAME       Specify the username associated with the file.`

var fileAccessionUsage = `Usage: sda-admin file set-accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
  Assign accession ID to a file and associate it with a user.

Options:
  -filepath FILEPATH   Specify the path of the file to assign the accession ID.
  -user USERNAME       Specify the username associated with the file.
  -accession-id ID     Specify the accession ID to assign to the file.`

var datasetUsage = `Create a dataset:
  Usage: sda-admin dataset create -dataset-id DATASET_ID [ACCESSION_ID ...]
    Create a dataset from a list of accession IDs and a dataset ID.
    
Release a dataset:
  Usage: sda-admin dataset release -dataset-id DATASET_ID
    Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID   Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]       (For dataset create) Specify one or more accession IDs to include in the dataset.

Use 'sda-admin help dataset <command>' for information on a specific command.`

var datasetCreateUsage = `Usage: sda-admin dataset create -dataset-id DATASET_ID [ACCESSION_ID ...]
  Create a dataset from a list of accession IDs and a dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]         (For dataset create) Specify one or more accession IDs to include in the dataset.`

var datasetReleaseUsage = `Usage: sda-admin dataset release -dataset-id DATASET_ID
  Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.`

var versionUsage = `Usage: sda-admin version
  Show the version information for sda-admin.`

func printVersion() {
	fmt.Printf("sda-admin version %s\n", version)
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

	if flag.Arg(0) == "help" {
		return nil
	}

	// Check environment variables if flags are not provided
	if apiURI == "" {
		apiURI = os.Getenv("API_HOST")
		if apiURI == "" {
			return fmt.Errorf("error: either -uri must be provided or API_HOST environment variable must be set")
		}
	}

	if token == "" {
		token = os.Getenv("ACCESS_TOKEN")
		if token == "" {
			return fmt.Errorf("error: either -token must be provided or ACCESS_TOKEN environment variable must be set")
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
	case "version":
		fmt.Println(versionUsage)
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
	var filepath, username string
	fileIngestCmd.StringVar(&filepath, "filepath", "", "Filepath to ingest")
	fileIngestCmd.StringVar(&username, "user", "", "Username to associate with the file")

	if err := fileIngestCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	if filepath == "" || username == "" {
		return fmt.Errorf("error: both -filepath and -user are required.\n%s", fileIngestUsage)
	}

	if err := helpers.CheckValidChars(filepath); err != nil {
		return err
	}

	err := file.Ingest(apiURI, token, username, filepath)
	if err != nil {
		return fmt.Errorf("error: failed to ingest file, reason: %v", err)
	}

	return nil
}

func handleFileAccessionCommand() error {
	fileAccessionCmd := flag.NewFlagSet("set-accession", flag.ExitOnError)
	var filepath, username, accessionID string
	fileAccessionCmd.StringVar(&filepath, "filepath", "", "Filepath to assign accession ID")
	fileAccessionCmd.StringVar(&username, "user", "", "Username to associate with the file")
	fileAccessionCmd.StringVar(&accessionID, "accession-id", "", "Accession ID to assign")

	if err := fileAccessionCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	if filepath == "" || username == "" || accessionID == "" {
		return fmt.Errorf("error: -filepath, -user, and -accession-id are required.\n%s", fileAccessionUsage)
	}

	if err := helpers.CheckValidChars(filepath); err != nil {
		return err
	}

	err := file.SetAccession(apiURI, token, username, filepath, accessionID)
	if err != nil {
		return fmt.Errorf("error: failed to assign accession ID to file, reason: %v", err)
	}

	return nil
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
	var datasetID string
	datasetCreateCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to create")

	if err := datasetCreateCmd.Parse(flag.Args()[2:]); err != nil {
		return fmt.Errorf("error: failed to parse command line arguments, reason: %v", err)
	}

	accessionIDs := datasetCreateCmd.Args() // Args() returns the non-flag arguments after parsing

	if datasetID == "" || len(accessionIDs) == 0 {
		return fmt.Errorf("error: -dataset-id and at least one accession ID are required.\n%s", datasetCreateUsage)
	}

	err := dataset.Create(apiURI, token, datasetID, accessionIDs)
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
	default:
		fmt.Fprintf(os.Stderr, "unknown command '%s'.\n%s\n", flag.Arg(0), usage)
		os.Exit(1)
	}

	os.Exit(0)
}
