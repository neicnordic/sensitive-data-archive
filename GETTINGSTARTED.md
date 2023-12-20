## Getting Started developing components of the SDA stack

Should one wish to engage in the development of the SDA stack itself, the prerequisite is the installation of [Go](https://www.golang.org/) on the respective machine.
The recommended version can be checked by running:

```sh
$ make go-version-check
...
```

In preparation for local development, it is essential to verify the proper installation of Go, including the establishment of a [GOPATH](https://golang.org/doc/code.html#GOPATH). Confirm that $GOPATH/bin is included in the system's path, as certain distributions may package outdated versions of build tools. Subsequently, proceed to clone the repository. SDA employs [Go Modules](https://github.com/golang/go/wiki/Modules), and it is advisable to perform the cloning operation outside the GOPATH. Following this, obtain any necessary build tools by initializing the environment through bootstrapping:

```sh
$ make bootstrap
...
```

### Makefile options

The Makefile is primarily designed to be an aid during development work.

#### Building the containers

To build all containers for the SDA stack:

```sh
$ make build-all
...
```

To build the container for a specific component replace `all` with the folder name:

```sh
$ make build-<folder-name>
...
```

#### Running the integration tests

This will build the container and run the integration test for the PostgreSQL container. The same test will run on every PR in github:

```sh
$ make integrationtest-postgres
...
```

This will build the RabbitMQ and SDA containers and run the integration test for the RabbitMQ container. The same test will run on every PR in github:

```sh
$ make integrationtest-rabbitmq
...
```

This will build all containers and run the integration tests for the SDA stack. The same test will run on every PR in github:

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

To run golangci-lint for a specific component replace `all` with the folder name (`sda`, `sda-auth`, `sda-download`):

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

To run the static code tests for a specific component replace `all` with the folder name (`sda`, `sda-auth`, `sda-download`, `sda-sftp-inbox`):

```sh
$ make test-<folder-name>
...
```
