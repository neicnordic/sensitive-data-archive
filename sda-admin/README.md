# sda-admin

`sda-admin` is a command-line tool for managing sensitive data archives. It provides functionalities to list users and files, ingest and set accession IDs for files, and create or release datasets.

## General Usage

```sh
sda-admin [-uri URI] [-token TOKEN] <command> [options]
```

## Global Options
- `-uri URI`
Set the URI for the API server (optional if the environmental variable `API_HOST` is set).
- `-token TOKEN`
Set the authentication token (optional if the environmental variable `ACCESS_TOKEN` is set).

## List all users

Use the following command to return all users with active uploads as a JSON array 
```sh
sda-admin list users 
```

## List all files, optionally filtered by a specific user.

Use the following command to return all files belonging to the user associated with the token

```sh
sda-admin list files 
```

Use the following command to return all files belonging to the specified user `test@dummy.org`
```sh
sda-admin list files -user test@dummy.org
```

## Ingest a file

Use the following command to trigger the ingesting of a given file `/path/to/file.c4gh` that belongs to the user `test@dummy.org` 

```sh
sda-admin file ingest -filepath /path/to/file.c4gh -user test@dummy.org 
```

## Assign an accession ID to a file

Use the following command to assign an accession ID `my-accession-id-1` to a given file `/path/to/file.c4gh` that belongs to the user `test@dummy.org`

```sh
sda-admin file accession -filepath /path/to/file.c4gh -user test@dummy.org -accession-id my-accession-id-1 
```

## Create a dataset from a list of accession IDs and the dataset ID

Use the following command to create a dataset `dataset001` from accession IDs `my-accession-id-1` and `my-accession-id-2`

```sh
sda-admin dataset create -dataset-id dataset001 my-accession-id-1 my-accession-id-2 
```


## Release a dataset for downloading

Use the following command to release the dataset `dataset001` for downloading

```sh
sda-admin dataset release -dataset-id dataset001
```

## Show version information

Use the following command to show the version information for sda-admin.

```sh
sda-admin version
```

## Help

For detailed usage information about specific commands or options, use:

```sh
sda-admin help <command>
```

### Examples 

To get help on the list command:
```sh
sda-admin help list
```

To get help on the file ingest command:

```sh
sda-admin help file ingest
```

To get help on the dataset create command:

```sh
sda-admin help dataset create
```