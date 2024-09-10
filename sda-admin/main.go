package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/sda-admin/dataset"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/file"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/helpers"
	"github.com/neicnordic/sensitive-data-archive/sda-admin/list"
)

var version = "1.0.0"

var (
	apiURI string
	token  string
)

// Command-line usage
var usage = `
Usage:
  sda-admin [-uri URI] [-token TOKEN] <command> [options]

Commands:
  list users                     List all users
  list files -user USERNAME      List all files for a specified user
  file ingest -filepath FILEPATH -user USERNAME
                                 Trigger ingestion of the given file
  file accession -filepath FILEPATH -user USERNAME -accession-id accessionID
                                 Assign accession ID to a file
  dataset create -dataset-id DATASET_ID accessionID [accessionID ...]
                                 Create a dataset from a list of accession IDs and the dataset ID
  dataset release -dataset-id DATASET_ID
                                 Release a dataset for downloading
  
Global Options:
  -uri URI         Set the URI for the API server (optional if API_HOST is set)
  -token TOKEN     Set the authentication token (optional if ACCESS_TOKEN is set)

Additional Commands:
  help             Show this help message
  -h, -help        Show this help message
`

var listUsage = `
List Users:
  Usage: sda-admin list users
    List all users in the system.

List Files:
  Usage: sda-admin list files -user USERNAME
    List all files for a specified user.
  
Options:
  -user USERNAME    (For list files) List files owned by the specified user

Use 'sda-admin help list <command>' for information on a specific list command.
`

var listUsersUsage = `
Usage: sda-admin list users
  List all users in the system.
`

var listFilesUsage = `
Usage: sda-admin list files -user USERNAME
  List all files for a specified user.

Options:
  -user USERNAME    List files owned by the specified user
`

var fileUsage = `
Ingest a File:
  Usage: sda-admin file ingest -filepath FILEPATH -user USERNAME
    Trigger the ingestion of a given file for a specific user.

Accession a File:
  Usage: sda-admin file accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
    Assign an accession ID to a file and associate it with a user.

Common Options for 'file' Commands:
  -filepath FILEPATH    Specify the path of the file.
  -user USERNAME        Specify the username associated with the file.

  For 'file accession' additionally:
  -accession-id ID      Specify the accession ID to assign to the file.

Use 'sda-admin help file <command>' for information on a specific command.
`

var fileIngestUsage = `
Usage: sda-admin file ingest -filepath FILEPATH -user USERNAME
  Trigger ingestion of the given file for a specific user.

Options:
  -filepath FILEPATH    Specify the path of the file to ingest.
  -user USERNAME        Specify the username associated with the file.
`

var fileAccessionUsage = `
Usage: sda-admin file accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
  Assign accession ID to a file and associate it with a user.

Options:
  -filepath FILEPATH    Specify the path of the file to assign the accession ID.
  -user USERNAME        Specify the username associated with the file.
  -accession-id ID      Specify the accession ID to assign to the file.
`

var datasetUsage = `
Create a Dataset:
  Usage: sda-admin dataset create -dataset-id DATASET_ID [ACCESSION_ID ...]
    Create a dataset from a list of accession IDs and the dataset ID.
    
Release a Dataset:
  Usage: sda-admin dataset release -dataset-id DATASET_ID
    Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]         (For dataset create) Specify one or more accession IDs to include in the dataset.

Use 'sda-admin help dataset <command>' for information on a specific dataset command.
`

var datasetCreateUsage = `
Usage: sda-admin dataset create -dataset-id DATASET_ID [ACCESSION_ID ...]
  Create a dataset from a list of accession IDs and the dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]         (For dataset create) Specify one or more accession IDs to include in the dataset.
`

var datasetReleaseUsage = `
Usage: sda-admin dataset release -dataset-id DATASET_ID
  Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset to release.
`

var versionUsage = `
Usage: sda-admin version
  Show the version information for sda-admin.
`

func printVersion() {
	fmt.Printf("sda-admin version %s\n", version)
}

func checkToken(token string) error {
	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	return nil
}

func parseFlagsAndEnv() error {
	// Set up flags
	flag.StringVar(&apiURI, "uri", "", "Set the URI for the SDA server (optional if API_HOST is set)")
	flag.StringVar(&token, "token", "", "Set the authentication token (optional if ACCESS_TOKEN is set)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}

	// Parse global flags first
	flag.Parse()

	// If no command is provided, show usage
	if flag.NArg() == 0 {
		return fmt.Errorf(usage)
	}

	// Check environment variables if flags are not provided
	if flag.Arg(0) != "help" {
		if apiURI == "" {
			apiURI = os.Getenv("API_HOST")
			if apiURI == "" {
				return fmt.Errorf("error: either -uri must be provided or API_HOST environment variable must be set.")
			}
		}

		if token == "" {
			token = os.Getenv("ACCESS_TOKEN")
			if token == "" {
				return fmt.Errorf("error: either -token must be provided or ACCESS_TOKEN environment variable must be set.")
			}
		}
	}

	return nil
}

func handleHelpCommand() error {
	if flag.NArg() > 1 {
		switch flag.Arg(1) {
		case "list":
			if err := handleHelpList(); err != nil {
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
			fmt.Fprint(os.Stderr, versionUsage)
		default:
			fmt.Fprintf(os.Stderr, "Unknown command '%s'.\n", flag.Arg(1))
			fmt.Fprint(os.Stderr, usage)
			return fmt.Errorf("")
		}
	} else {
		fmt.Fprint(os.Stderr, usage)
	}

	return nil
}

func handleHelpList() error {
	if flag.NArg() == 2 {
		fmt.Fprint(os.Stderr, listUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "users" {
		fmt.Fprint(os.Stderr, listUsersUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "files" {
		fmt.Fprint(os.Stderr, listFilesUsage)
	} else {
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		fmt.Fprint(os.Stderr, listUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleHelpFile() error {
	if flag.NArg() == 2 {
		fmt.Fprint(os.Stderr, fileUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "ingest" {
		fmt.Fprint(os.Stderr, fileIngestUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "accession" {
		fmt.Fprint(os.Stderr, fileAccessionUsage)
	} else {
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		fmt.Fprint(os.Stderr, fileUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleHelpDataset() error {
	if flag.NArg() == 2 {
		fmt.Fprint(os.Stderr, datasetUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "create" {
		fmt.Fprint(os.Stderr, datasetCreateUsage)
	} else if flag.NArg() > 2 && flag.Arg(2) == "release" {
		fmt.Fprint(os.Stderr, datasetReleaseUsage)
	} else {
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		fmt.Fprint(os.Stderr, datasetUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleListCommand() error {
	if flag.NArg() < 2 {
		fmt.Fprint(os.Stderr, "Error: 'list' requires a subcommand (users, files).\n")
		fmt.Fprint(os.Stderr, listUsage)
		return fmt.Errorf("")
	}
	switch flag.Arg(1) {
	case "users":
		if err := helpers.CheckTokenExpiration(token); err != nil {
			return err
		}
		err := list.Users(apiURI, token)
		if err != nil {
			return fmt.Errorf("Error: failed to get users, reason: %v\n", err)
		}
	case "files":
		if err := handleListFilesCommand(); err != nil {
			return err
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		fmt.Fprint(os.Stderr, listUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleListFilesCommand() error {
	listFilesCmd := flag.NewFlagSet("files", flag.ExitOnError)
	var username string
	listFilesCmd.StringVar(&username, "user", "", "Filter files by username")
	listFilesCmd.Parse(flag.Args()[2:])

	// Check if the -user flag was provided
	if username == "" {
		fmt.Fprint(os.Stderr, "Error: the -user flag is required.\n")
		fmt.Fprint(os.Stderr, listFilesUsage)
		return fmt.Errorf("")
	}

	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	if err := list.Files(apiURI, token, username); err != nil {
		return fmt.Errorf("Error: failed to get files, reason: %v\n", err)
	}

	return nil
}

func handleFileCommand() error {
	if flag.NArg() < 2 {
		fmt.Fprint(os.Stderr, "Error: 'file' requires a subcommand (ingest, accession).\n")
		fmt.Fprint(os.Stderr, fileUsage)
		return fmt.Errorf("")
	}
	switch flag.Arg(1) {
	case "ingest":
		if err := handleFileIngestCommand(); err != nil {
			return err
		}
	case "accession":
		if err := handleFileAccessionCommand(); err != nil {
			return err
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		fmt.Fprint(os.Stderr, fileUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleFileIngestCommand() error {
	fileIngestCmd := flag.NewFlagSet("ingest", flag.ExitOnError)
	var filepath, username string
	fileIngestCmd.StringVar(&filepath, "filepath", "", "Filepath to ingest")
	fileIngestCmd.StringVar(&username, "user", "", "Username to associate with the file")
	fileIngestCmd.Parse(flag.Args()[2:])

	if filepath == "" || username == "" {
		fmt.Fprint(os.Stderr, "Error: both -filepath and -user are required.\n")
		fmt.Fprint(os.Stderr, fileIngestUsage)
		return fmt.Errorf("")
	}

	if err := helpers.CheckValidChars(filepath); err != nil {
		return err
	}

	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	err := file.Ingest(apiURI, token, username, filepath)
	if err != nil {
		return fmt.Errorf("Error: failed to ingest file, reason: %v\n", err)
	} else {
		fmt.Println("File ingestion triggered successfully.")
	}

	return nil
}

func handleFileAccessionCommand() error {
	fileAccessionCmd := flag.NewFlagSet("accession", flag.ExitOnError)
	var filepath, username, accessionID string
	fileAccessionCmd.StringVar(&filepath, "filepath", "", "Filepath to assign accession ID")
	fileAccessionCmd.StringVar(&username, "user", "", "Username to associate with the file")
	fileAccessionCmd.StringVar(&accessionID, "accession-id", "", "Accession ID to assign")
	fileAccessionCmd.Parse(flag.Args()[2:])

	if filepath == "" || username == "" || accessionID == "" {
		fmt.Fprint(os.Stderr, "Error: -filepath, -user, and -accession-id are required.\n")
		fmt.Fprint(os.Stderr, fileAccessionUsage)
		return fmt.Errorf("")
	}

	if err := helpers.CheckValidChars(filepath); err != nil {
		return err
	}

	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	err := file.Accession(apiURI, token, username, filepath, accessionID)
	if err != nil {
		return fmt.Errorf("Error: failed to assign accession ID to file, reason: %v\n", err)
	} else {
		fmt.Println("Accession ID assigned to file successfully.")
	}

	return nil
}

func handleDatasetCommand() error {
	if flag.NArg() < 2 {
		fmt.Fprint(os.Stderr, "Error: 'dataset' requires a subcommand (create, release).\n")
		fmt.Fprint(os.Stderr, datasetUsage)
		return fmt.Errorf("")
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
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		fmt.Fprint(os.Stderr, datasetUsage)
		return fmt.Errorf("")
	}

	return nil
}

func handleDatasetCreateCommand() error {
	datasetCreateCmd := flag.NewFlagSet("create", flag.ExitOnError)
	var datasetID string
	datasetCreateCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to create")
	datasetCreateCmd.Parse(flag.Args()[2:])
	accessionIDs := datasetCreateCmd.Args() // Args() returns the non-flag arguments after parsing

	if datasetID == "" || len(accessionIDs) == 0 {
		fmt.Fprint(os.Stderr, "Error: -dataset-id and at least one accession ID are required.\n")
		fmt.Fprint(os.Stderr, datasetCreateUsage)
		return fmt.Errorf("")
	}

	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	err := dataset.Create(apiURI, token, datasetID, accessionIDs)
	if err != nil {
		return fmt.Errorf("Error: failed to create dataset, reason: %v\n", err)
	} else {
		fmt.Println("Dataset created successfully.")
	}

	return nil
}

func handleDatasetReleaseCommand() error {
	datasetReleaseCmd := flag.NewFlagSet("release", flag.ExitOnError)
	var datasetID string
	datasetReleaseCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to release")
	datasetReleaseCmd.Parse(flag.Args()[2:])

	if datasetID == "" {
		fmt.Fprint(os.Stderr, "Error: -dataset-id is required.\n")
		fmt.Fprint(os.Stderr, datasetReleaseUsage)
		return fmt.Errorf("")
	}

	if err := helpers.CheckTokenExpiration(token); err != nil {
		return err
	}

	err := dataset.Release(apiURI, token, datasetID)
	if err != nil {
		return fmt.Errorf("Error: failed to release dataset, reason: %v\n", err)
	} else {
		fmt.Println("Dataset released successfully.")
	}

	return nil
}

func main() {
	if err := parseFlagsAndEnv(); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "help", "-h", "-help":
		if err := handleHelpCommand(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
	case "list":
		if err := handleListCommand(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
	case "file":
		if err := handleFileCommand(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
	case "dataset":
		if err := handleDatasetCommand(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}
	case "version":
		printVersion()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command '%s'.\n", flag.Arg(0))
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	os.Exit(0)
}
