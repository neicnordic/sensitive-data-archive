# Sensitive Data Archive

`SDA` contains all components of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/) It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as a isolated Sensitive Data Archive.

For more information about the different components see the readme files in the respecive folders.

## Developing components of the SDA stack

If you wish to work on the SDA stack itself you'll first need [Go](https://www.golang.org/) installed on your machine.

For local dev first make sure Go is properly installed, including setting up a [GOPATH](https://golang.org/doc/code.html#GOPATH). Ensure that $GOPATH/bin is in your path as some distributions bundle the old version of build tools. Next, clone this repository. SDA uses [Go Modules](https://github.com/golang/go/wiki/Modules), so it is recommended that you clone the repository outside of the GOPATH. You can then download any required build tools by bootstrapping your environment:

```sh
$ make bootstrap
...
```

### Makefile options

The Makefile is primarily designd to be an aid during development work.

#### Building the containers

To build all containers for the SDA stack:

```sh
$ make build-all
...
```

To build the container for a speciffic component replace `all` with the folder name:

```sh
$ make build-<folder-name>
...
```

#### Running the integration tests

This will build the container and run the integration test for the postgresql container. The same test will run on every PR in github:

```sh
$ make integrationtest-postgres
...
```

This will build the rabbitmq and sda containers and run the integration test for the rabbitmq container. The same test will run on every PR in github:

```sh
$ make integrationtest-rabbitmq
...
```

This will build all containers and run the integration tests for the sda stack. The same test will run on every PR in github:

```sh
$ make integrationtest-sda
...
```

#### Linting the GO code

To run golangci-lint for all go components:

```sh
$ make lint-all
...
```

To run golangci-lint for a speciffic component replace `all` with the folder name (sda, sda-auth, sda-download):

```sh
$ make lint-<folder-name>
...
```

#### Running the static code tests

For the go code this means running `go test -count=1 ./...` in the target folder. For the *sftp-inbox* this calls `mvn test -B` inside a maven container.

To run the static code tests for all components:

```sh
$ make test-all
...
```

To run the static code tests for a speciffic component replace `all` with the folder name (sda, sda-auth, sda-download, sda-sftp-inbox):

```sh
$ make test-<folder-name>
...
```
