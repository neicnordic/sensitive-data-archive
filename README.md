# sda-pipeline

[![License](https://img.shields.io/github/license/neicnordic/sda-pipeline)](https://shields.io)
[![GoDoc](https://godoc.org/github.com/neicnordic/sda-pipeline?status.svg)](https://pkg.go.dev/github.com/neicnordic/sda-pipeline?tab=subdirectories)

[![Build Status](https://github.com/neicnordic/sda-pipeline/workflows/Go/badge.svg)](https://github.com/neicnordic/sda-pipeline/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/neicnordic/sda-pipeline)](https://goreportcard.com/report/github.com/neicnordic/sda-pipeline)
[![Code Coverage](https://img.shields.io/coveralls/github/neicnordic/sda-pipeline)](https://shields.io)

[![DeepSource](https://static.deepsource.io/deepsource-badge-light.svg)](https://deepsource.io/gh/neicnordic/sda-pipeline/?ref=repository-badge) [![Join the chat at https://gitter.im/neicnordic/sda-pipeline](https://badges.gitter.im/neicnordic/sda-pipeline.svg)](https://gitter.im/neicnordic/sda-pipeline?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

`sda-pipeline` is part of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/) and implements the components required for data submission.
It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as a isolated Sensitive Data Archive. 
`sda-pipeline` was built with support for both S3 and POSIX storage.

## Deployment

Recommended provisioning method for production is:

* on a `kubernetes cluster` using the [helm chart](https://github.com/neicnordic/sda-helm/);

For local development/testing see instructions in [dev_utils](/dev_utils) folder.
There is an README file in the [dev_utils](/dev_utils) folder with sections for running the pipeline locally using Docker Compose.

* [First run](./dev_utils/README.md#Getting-up-and-running-fast)
* [Production like run](./dev_utils/README.md#Starting-the-services-using-docker-compose-with-TLS-enabled)
* [Manual execution](./dev_utils/README.md#Manually-run-the-integration-test)

## Core Components

| Component     | Role |
|---------------|------|
| intercept     | The intercept service relays message between the queue provided from the federated service and local queues. **(Required only for Federated EGA use case)** |
| ingest        | The ingest service accepts messages for files uploaded to the inbox, registers the files in the database with their headers, and stores them header-stripped in the archive storage. |
| verify        | The verify service reads and decrypts ingested files from the archive storage and sends accession requests. |
| finalize      | The finalize command accepts messages with _accessionIDs_ for ingested files and registers them in the database. |
| mapper        | The mapper service registers the mapping of _accessionIDs_ (IDs for files) to _datasetIDs_. |
| backup          | The backup service accepts messages with _accessionIDs_ for ingested files and copies them to the second/backup storage. |

## Internal Components

| Component     | Role |
|---------------|------|
| broker        | Package containing communication with Message Broker [SDA-MQ](https://github.com/neicnordic/sda-mq). |
| config        | Package for managing configuration. |
| database      | Provides functionalities for using the database, as well as high level functions for working with the [SDA-DB](https://github.com/neicnordic/sda-db). |
| storage       | Provides interface for storage areas such as a regular file system (POSIX) or as a S3 object store. |

## Documentation

`sda-pipeline` documentation can be found at: https://neicnordic.github.io/sda-pipeline/pkg/sda-pipeline/

NeIC Sensitive Data Archive documentation can be found at: https://neic-sda.readthedocs.io/en/latest/ 
along with documentation about other components for data access.

## Contributing

We happily accepts contributions. Please see our [contributing documentation](CONTRIBUTING.md) for some tips on getting started.
