[![CodeQL](https://github.com/neicnordic/sda-download/actions/workflows/codeql-analysis.yml/badge.svg)](https://github.com/neicnordic/sda-download/actions/workflows/codeql-analysis.yml)
[![Tests](https://github.com/neicnordic/sda-download/actions/workflows/test.yml/badge.svg)](https://github.com/neicnordic/sda-download/actions/workflows/test.yml)
[![Multilinters](https://github.com/neicnordic/sda-download/actions/workflows/report.yml/badge.svg)](https://github.com/neicnordic/sda-download/actions/workflows/report.yml)
[![integration tests](https://github.com/neicnordic/sda-download/actions/workflows/integration.yml/badge.svg)](https://github.com/neicnordic/sda-download/actions/workflows/integration.yml)
[![codecov](https://codecov.io/gh/neicnordic/sda-download/branch/main/graph/badge.svg?token=ZHO4XCDPJO)](https://codecov.io/gh/neicnordic/sda-download)

# SDA Download
`sda-download` is a `go` implementation of the [Data Out API](https://neic-sda.readthedocs.io/en/latest/dataout.html#rest-api-endpoints). The [API Reference](docs/API.md) has example requests and responses.

## Deployment

Recommended provisioning method for production is:

* on a `kubernetes cluster` using the [helm chart](https://github.com/neicnordic/sda-helm/);

For local development/testing see instructions in [dev_utils](/dev_utils) folder.
There is an README file in the [dev_utils](/dev_utils) folder with sections for running the pipeline locally using Docker Compose.

* [First run](./dev_utils/README.md#Getting-up-and-running-fast)
* [Production like run](./dev_utils/README.md#Starting-the-services-using-docker-compose-with-TLS-enabled)
* [Manual execution](./dev_utils/README.md#Manually-run-the-integration-test)


## API Components

| Component     | Role |
|---------------|------|
| middleware     | Performs access token verification and validation |
| sda        | Constructs the main API endpoints for the NeIC SDA Data Out API. |


## Internal Components

| Component     | Role |
|---------------|------|
| config        | Package for managing configuration. |
| database      | Provides functionalities for using the database, as well as high level functions for working with the [SDA-DB](https://github.com/neicnordic/sda-db). |
| storage       | Provides interface for storage areas such as a regular file system (POSIX) or as a S3 object store. |
| session       | DatasetCache stores the dataset permissions and information whether this information has already been checked or not. This information can then be used to skip the time-costly authentication middleware |

## Package Components

| Component     | Role |
|---------------|------|
| auth        | Auth pkg is used by the middleware to parse OIDC Details and extract GA4GH Visas from a [GA4GH Passport](https://github.com/ga4gh-duri/ga4gh-duri.github.io/blob/master/researcher_ids/ga4gh_passport_v1.md) |
| request       | This pkg Stores a HTTP client, so that it doesn't need to be initialised on every request. |
