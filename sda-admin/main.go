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
	token   string
)

// Command-line usage
var usage = `USAGE: 
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

func listUsage() {
	usageText := `
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
	fmt.Println(usageText)
}

func listUsersUsage() {
	usageText := `Usage: sda-admin list users
  List all users in the system.
`
	fmt.Println(usageText)
}

func listFilesUsage() {
	usageText := `Usage: sda-admin list files -user USERNAME
  List all files for a specified user.

Options:
  -user USERNAME    List files owned by the specified user
`
	fmt.Println(usageText)
}

func fileUsage() {
	usageText := `
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
	fmt.Println(usageText)
}

func fileIngestUsage() {
	usageText := `Usage: sda-admin file ingest -filepath FILEPATH -user USERNAME
  Trigger ingestion of the given file for a specific user.

Options:
  -filepath FILEPATH    Specify the path of the file to ingest.
  -user USERNAME        Specify the username associated with the file.
`
	fmt.Println(usageText)
}

func fileAccessionUsage() {
	usageText := `Usage: sda-admin file accession -filepath FILEPATH -user USERNAME -accession-id ACCESSION_ID
  Assign accession ID to a file and associate it with a user.

Options:
  -filepath FILEPATH    Specify the path of the file to assign the accession ID.
  -user USERNAME        Specify the username associated with the file.
  -accession-id ID      Specify the accession ID to assign to the file.
`
	fmt.Println(usageText)
}

func datasetUsage() {
	usageText := `
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
	fmt.Println(usageText)
}

func datasetCreateUsage() {
	usageText := `Usage: sda-admin dataset create -dataset-id DATASET_ID [ACCESSION_ID ...]
  Create a dataset from a list of accession IDs and the dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset.
  [ACCESSION_ID ...]         (For dataset create) Specify one or more accession IDs to include in the dataset.
`
	fmt.Println(usageText)
}

func datasetReleaseUsage() {
	usageText := `Usage: sda-admin dataset release -dataset-id DATASET_ID
  Release a dataset for downloading based on its dataset ID.

Options:
  -dataset-id DATASET_ID    Specify the unique identifier for the dataset to release.
`
	fmt.Println(usageText)
}

func versionUsage() {
	usageText := `Usage: sda-admin version
  Show the version information for sda-admin.
`
	fmt.Println(usageText)
}

func printVersion() {
	fmt.Printf("sda-admin version %s\n", version)
}

func checkToken(token string) {
	if err := helpers.CheckTokenExpiration(token); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlagsAndEnv() {
	// Set up flags
	flag.StringVar(&apiURI, "uri", "", "Set the URI for the SDA server (optional if API_HOST is set)")
	flag.StringVar(&token, "token", "", "Set the authentication token (optional if ACCESS_TOKEN is set)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Println(usage)
	}

	// Parse global flags first
	flag.Parse()

	// If no command or help command is provided, show usage
	if flag.NArg() == 0 || (flag.NArg() == 1 && flag.Arg(0) == "help") {
		flag.Usage()
		os.Exit(0)
	}

	// Check environment variables if flags are not provided
	if flag.Arg(0) != "help" {
		if apiURI == "" {
			apiURI = os.Getenv("API_HOST")
			if apiURI == "" {
				fmt.Println("Error: either -uri must be provided or API_HOST environment variable must be set.")
				os.Exit(1)
			}
		}

		if token == "" {
			token = os.Getenv("ACCESS_TOKEN")
			if token == "" {
				fmt.Println("Error: either -token must be provided or ACCESS_TOKEN environment variable must be set.")
				os.Exit(1)
			}
		}
	}
}

func handleHelpCommand() {
	if flag.NArg() > 1 {
		switch flag.Arg(1) {
		case "list":
			handleHelpList()
		case "file":
			handleHelpFile()
		case "dataset":
			handleHelpDataset()
		case "version":
			versionUsage()
			os.Exit(0)
		default:
			fmt.Printf("Unknown command '%s'.\n", flag.Arg(1))
			flag.Usage()
		}
	} else {
		flag.Usage()
	}
	os.Exit(0)
}

func handleHelpList() {
	if flag.NArg() == 2 {
		listUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "users" {
		listUsersUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "files" {
		listFilesUsage()
	} else {
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		listUsage()
	}
}

func handleHelpFile() {
	if flag.NArg() == 2 {
		fileUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "ingest" {
		fileIngestUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "accession" {
		fileAccessionUsage()
	} else {
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		fileUsage()
	}
}

func handleHelpDataset() {
	if flag.NArg() == 2 {
		datasetUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "create" {
		datasetCreateUsage()
	} else if flag.NArg() > 2 && flag.Arg(2) == "release" {
		datasetReleaseUsage()
	} else {
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(2), flag.Arg(1))
		datasetUsage()
	}
}

func handleListCommand() {
	if flag.NArg() < 2 {
		fmt.Println("Error: 'list' requires a subcommand (users, files).")
		listUsage()
		os.Exit(1)
	}
	switch flag.Arg(1) {
	case "users":
		checkToken(token)
		err := list.ListUsers(apiURI, token)
		if err != nil {
			fmt.Printf("Error: failed to get users, reason: %v\n", err)
		}
	case "files":
		handleListFilesCommand()
	default:
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		listUsage()
		os.Exit(1)
	}
}

func handleListFilesCommand() {
	listFilesCmd := flag.NewFlagSet("files", flag.ExitOnError)
	var username string
	listFilesCmd.StringVar(&username, "user", "", "Filter files by username")
	listFilesCmd.Parse(flag.Args()[2:])

	// Check if the -user flag was provided
	if username == "" {
		fmt.Println("Error: the -user flag is required.")
		listFilesUsage()
		os.Exit(1)
	}

	checkToken(token)
	err := list.ListFiles(apiURI, token, username)
	if err != nil {
		fmt.Printf("Error: failed to get files, reason: %v\n", err)
	}
}

func handleFileCommand() {
	if flag.NArg() < 2 {
		fmt.Println("Error: 'file' requires a subcommand (ingest, accession).")
		fileUsage()
		os.Exit(1)
	}
	switch flag.Arg(1) {
	case "ingest":
		handleFileIngestCommand()
	case "accession":
		handleFileAccessionCommand()
	default:
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		fileUsage()
		os.Exit(1)
	}
}

func handleFileIngestCommand() {
	fileIngestCmd := flag.NewFlagSet("ingest", flag.ExitOnError)
	var filepath, username string
	fileIngestCmd.StringVar(&filepath, "filepath", "", "Filepath to ingest")
	fileIngestCmd.StringVar(&username, "user", "", "Username to associate with the file")
	fileIngestCmd.Parse(flag.Args()[2:])

	if filepath == "" || username == "" {
		fmt.Println("Error: both -filepath and -user are required.")
		fileIngestUsage()
		os.Exit(1)
	}

	checkToken(token)
	err := file.FileIngest(apiURI, token, username, filepath)
	if err != nil {
		fmt.Printf("Error: failed to ingest file, reason: %v\n", err)
	} else {
		fmt.Println("File ingestion triggered successfully.")
	}
}

func handleFileAccessionCommand() {
	fileAccessionCmd := flag.NewFlagSet("accession", flag.ExitOnError)
	var filepath, username, accessionID string
	fileAccessionCmd.StringVar(&filepath, "filepath", "", "Filepath to assign accession ID")
	fileAccessionCmd.StringVar(&username, "user", "", "Username to associate with the file")
	fileAccessionCmd.StringVar(&accessionID, "accession-id", "", "Accession ID to assign")
	fileAccessionCmd.Parse(flag.Args()[2:])

	if filepath == "" || username == "" || accessionID == "" {
		fmt.Println("Error: -filepath, -user, and -accession-id are required.")
		fileAccessionUsage()
		os.Exit(1)
	}

	checkToken(token)
	err := file.FileAccession(apiURI, token, username, filepath, accessionID)
	if err != nil {
		fmt.Printf("Error: failed to assign accession ID to file, reason: %v\n", err)
	} else {
		fmt.Println("Accession ID assigned to file successfully.")
	}
}

func handleDatasetCommand() {
	if flag.NArg() < 2 {
		fmt.Println("Error: 'dataset' requires a subcommand (create, release).")
		datasetUsage()
		os.Exit(1)
	}
	switch flag.Arg(1) {
	case "create":
		handleDatasetCreateCommand()
	case "release":
		handleDatasetReleaseCommand()
	default:
		fmt.Printf("Unknown subcommand '%s' for '%s'.\n", flag.Arg(1), flag.Arg(0))
		datasetUsage()
		os.Exit(1)
	}
}

func handleDatasetCreateCommand() {
	datasetCreateCmd := flag.NewFlagSet("create", flag.ExitOnError)
	var datasetID string
	datasetCreateCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to create")
	datasetCreateCmd.Parse(flag.Args()[2:])
	accessionIDs := datasetCreateCmd.Args() // Args() returns the non-flag arguments after parsing

	if datasetID == "" || len(accessionIDs) == 0 {
		fmt.Println("Error: -dataset-id and at least one accession ID are required.")
		datasetCreateUsage()
		os.Exit(1)
	}

	checkToken(token)
	err := dataset.DatasetCreate(apiURI, token, datasetID, accessionIDs)
	if err != nil {
		fmt.Printf("Error: failed to create dataset, reason: %v\n", err)
	} else {
		fmt.Println("Dataset created successfully.")
	}
}

func handleDatasetReleaseCommand() {
	datasetReleaseCmd := flag.NewFlagSet("release", flag.ExitOnError)
	var datasetID string
	datasetReleaseCmd.StringVar(&datasetID, "dataset-id", "", "ID of the dataset to release")
	datasetReleaseCmd.Parse(flag.Args()[2:])

	if datasetID == "" {
		fmt.Println("Error: -dataset-id is required.")
		datasetReleaseUsage()
		os.Exit(1)
	}

	checkToken(token)
	err := dataset.DatasetRelease(apiURI, token, datasetID)
	if err != nil {
		fmt.Printf("Error: failed to release dataset, reason: %v\n", err)
	} else {
		fmt.Println("Dataset released successfully.")
	}
}

func main() {
	parseFlagsAndEnv()

	switch flag.Arg(0) {
	case "help", "-h", "-help":
		handleHelpCommand()
	case "list":
		handleListCommand()
	case "file":
		handleFileCommand()
	case "dataset":
		handleDatasetCommand()
	case "version":
		printVersion()
		os.Exit(0)
	default:
		fmt.Printf("Unknown command '%s'.\n", flag.Arg(0))
		flag.Usage()
		os.Exit(1)
	}
}
