# Changelog - Sensitive Data Archive

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- s3inbox: Allow forwarding of the [HeadObject action](https://docs.aws.amazon.com/AmazonS3/latest/API/API_HeadObject.html)
- api: add new API /dataset/{dataset_id} for getting state of a dataset

### Changed

- ingest:
  - Use db transactions during cancel and ingest actions and rollback if encounter error.
  - Requeue messages which could be expected to succeed on a retry.
  - Add a "error-queue-reason" header when sending messages to the error queue which can not be retried to record reason.
  - Update unit tests to use mocks of the db, and storage reader, writer instead of docker / temp directory.
    - Remove redundant unit tests which are covered by other tests, and remove unit tests which should be covered by integration tests.
  - Use log/slog for logging instead of logrus.
- Update remaining mocks to implement github.com/stretchr/testify/mock.Mock.
- inbox project code:
  - **Breaking:** `storage.inbox.projectCode` and `storage.inbox.projectCodeDelimiter` are renamed to `inboxpath.project_code` and `inboxpath.project_delimiter`. A deployment that sets the old keys silently falls back to the stock inbox layout, so update the config before upgrading.
  - New `internal/inboxpath` package owns the layout: it registers both keys through `config/v2`, so they appear in `--help`, and carries `ResolveInboxPath`. Services call `inboxpath.Load()` once at startup to validate, then resolve paths without holding any config of their own.
  - `helper.ResolveInboxPath` and `helper.UnanonymizeFilepath` moved into that package, so `internal/config` no longer imports `internal/helper`. `helper.AnonymizeFilepath` stays where it is.
  - Only ingest, mapper and api validate the section now. finalize, verify and intercept never read it and no longer fail at startup on a malformed one.

### Fixed

- Fixed downloading files by the `file_dataset.download_path` in the sda-download(v1) 
- s3 writer: don't panic or upload to an empty bucket name when every endpoint is full
- api: fix publishing to correct destination on POST /dataset/release/{datasetid}
- api: resolve the inbox path through the configured project-code layout again when deleting and downloading inbox files. The `gin` -> `net/http` rewrite in 3.1.75 reverted both call sites to the stock `UnanonymizeFilepath`, so on a deployment with `storage.inbox.projectCode` set they addressed a directory that does not exist: the download returned 500, and on an s3 inbox the delete reported success while leaving the file in place.

## [3.1.76] - 2026-07-15

### Added

- Added functionality to override the exposed download path for a file within a dataset
  - The file download path for a file in a dataset is set during the dataset creation(ie when a file is added to a dataset)
  - The download and download-v2 services will default to the submission file path if file download path is not set for a dataset
  - Updated [sda api swagger_v1.yml](cmd/api/swagger_v1.yml) DatasetCreate to allow caller to override the file download path in a dataset by the file accession
  - Updated [dataset-mapping schema](schemas/isolated/dataset-mapping.json) to allow propagation of the file download path per file accession
  - Added [new column to file_dataset table and bumped schema version to 25](../postgresql/migratedb.d/25_add_download_path_column_to_file_dataset.sql)

## [3.1.75] - 2026-07-08

### Added

- Standalone config package: `/cmd/api/config/config.go` to handle configuration for the `api` service 
- Shared mock packages `/mocks` that hold mock implementations of `database.go` and `broker.go` to be used for unit testing

### Changed

- `api.go` to use `v2/broker` package
- `api.go` to use `net/http` instead of `gin-gonic/gin` for routing
- `api_test.go` to make use of interfaces for `broker.go` and `database.go` to be able to run tests in isolation without docker instances of those services
- `api_test.go` to run table-driven test cases for each handler / endpoint

## [3.1.74] - 2026-06-22

### Changed

- Updated the sda-api `dataset/create` API to not reject requests if the requested files belong to different users.
- Updated [sda api swagger_v1.yml](cmd/api/swagger_v1.yml) to not specify user in the DatasetCreate as it is no longer needed.
- Updated sda-download to handle file downloads when a file exists in multiple datasets. Allows file download if user has a visa for at least one dataset the file is present in.

## [3.1.73] - 2026-06-16

### Added

- Configurable project-code inbox paths: `storage.inbox.projectCode` and
  `storage.inbox.projectCodeDelimiter` reconstruct the physical per-user inbox directory
  (`<projectCode><delimiter><username>/...`) from an anonymized submission path. Defaults are
  empty, so the stock inbox layout is unchanged.

### Fixed

- Ingest: a file first registered by the ingest service (the non-s3inbox `status ""` path) was
  written to the database but never archived. Restored reading the submission file path (not the
  broker correlation id) and the fall-through to archive after registration.

## [3.1.72] - 2026-05-29

### Fixed

- Fixed Unhandled error linter issues in sda/download

## [3.1.71] - 2026-05-25
- Started keeping a changelog after this version.
