# Sensitive Data Archive

The `SDA` contains all components of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/). It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as a standalone Sensitive Data Archive.

For more information about the different components, please refer to the README files in their respective folders.

## How to run the SDA stack 
The following instructions outline the steps to set up and run the `SDA` services for development and testing using Docker. These steps are based on the provided [Makefile](./Makefile) commands.

### Prerequisites
Ensure you have the following installed on your system:

- [`Go`](https://www.golang.org/): The required version is specified in the `sda` Dockerfile. Verify using
    ```sh
    $ make go-version-check
    ```

- Docker: Version 24 or higher. Verify using 
    ```sh
    $ make docker-version-check 
    ```
- Docker Compose: Version 2 or higher. For Linux, ensure the [Compose plugin](https://docs.docker.com/compose/install/linux/) is installed.

In preparation for local development, it is essential to verify that `$GOPATH/bin` is part of the system PATH, as certain distributions may package outdated versions of build tools. SDA uses [Go Modules](https://github.com/golang/go/wiki/Modules), and it is advisable to clone the repository outside the `GOPATH`. After cloning, initialize the environment and obtain necessary build tools using the bootstrap command: 

```sh
$ make bootstrap
```

### Build Docker images 

Build the required Docker images for all SDA services:

```sh
$ make build-all
```

You can also build images for individual services by replacing `all` with the folder name (`postgresql`, `rabbitmq`, `sda`, `sda-download`, `sda-sftp-inbox`), for example

```sh
$ make build-sda
```

To build the CLI for `sda-admin`:

```sh
$ make build-sda-admin
```

### Running the services

#### Start services with Docker Compose
The following command will build all required images, bring up all services using the Docker Compose file [sda-s3-integration.yml](.github/integration/sda-s3-integration.yml) (configured for S3 as the storage method) and run the integration test:

```sh
$ make integrationtest-sda-s3-run
```

#### Shut down all services and clean up resources
The following command will shut down all services and clean up all related resources:

```sh
$ make integrationtest-sda-s3-down
```

For the setup with POSIX as the storage method, use 
`make integrationtest-sda-posix-run` and `make integrationtest-sda-posix-down` to start and shut down services. For the setup including the [`sync`](https://github.com/neicnordic/sda-sync) service, use `make integrationtest-sda-sync-run` and `make integrationtest-sda-sync-down` to start and shut down services.

#### Running the integration tests
This will build all required images, bring up the services, run the integration test, and then shut down services and clean up resources. The same test runs on every pull request (PR) in GitHub.

- Integration test for the database:
    ```sh
    make integrationtest-postgres
    ```
- Integration test for RabbitMQ:
    ```sh
    make integrationtest-rabbitmq
    ```
- Integration test for all SDA setups (including S3, POSIX and sync):
    ```sh
    make integrationtest-sda
    ```
- Integration test for SDA using POSIX as the storage method:
    ```sh
    make integrationtest-sda-posix
    ```
- Integration test for SDA using S3 as the storage method:
    ```sh
    make integrationtest-sda-s3
    ```
- Integration test for SDA including the sync service:
    ```sh
    make integrationtest-sda-sync
    ```

### Linting the Go code

To run `golangci-lint` for all Go components:

```sh
$ make lint-all
```

To run `golangci-lint` for a specific component, replace `all` with the folder name (`sda`, `sda-auth`, `sda-download`), for example:

```sh
$ make lint-sda
```

### Running the static code tests

For Go code, this means running `go test -count=1 ./...` in the target folder. For the *sftp-inbox* this calls `mvn test -B` inside a Maven container.

To run the static code tests for all components:

```sh
$ make test-all
```

To run the static code tests for a specific component, replace `all` with the folder name (`sda`, `sda-admin`, `sda-download`, `sda-sftp-inbox`), for example:

```sh
$ make test-sda
```