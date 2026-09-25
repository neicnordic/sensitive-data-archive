# Changelog - Sensitive Data Archive

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Add observability package which adds support for tracing and metrics collecting and exporting with OpenTelemetry
  - See [Observability README.md](internal/observability/README.md) for additional information
  - Add OpenTelemetry instrumentation for the PostgreSQL database connection
  - Add support for injecting/extracting trace headers on the broker rabbitmq to support distributed tracing
  - Add tracing in the storage reader/writer libraries for s3 and posix
- Initialize observability package and otel middleware for http/grpc client and servers, and add span creating in the following
  - api, auth, download, finalize, ingest, mapper, reencrypt, rotatekey, s3inbox, sync, and verify

## [4.0.1] - 2026-09-24

### Fixed
- ingest, finalize: Large files no longer exceed `idle_in_transaction_session_timeout` and loop re-streaming; no storage I/O happens while a database transaction is open
  - ingest: register a file that is not known in the db (but present in the inbox) as "uploaded" in its own transaction before streaming, so a requeued message is retried
  - ingest: read the archived file size and write the "submitted" file event after the archive write
  - finalize: commit the backup in its own transaction before setting the accession ID
  - finalize: re-check the file status under the row lock so a cancel during the backup copy is not overwritten by "backed up" or "ready"
  - both: remove the freshly written archive/backup object when the database work fails before the commit is attempted

## [4.0.0] - 2026-09-21

### Added
- s3inbox: Allow forwarding of the [HeadObject action](https://docs.aws.amazon.com/AmazonS3/latest/API/API_HeadObject.html)
- api: add new API /dataset/{dataset_id} for getting state of a dataset
- api: add new endpoint /file/cancel for cancelling an ingested file.
- auth: `OIDC_ACRVALUES` for requiring an authentication context at OIDC login, e.g. two factor authentication.

### Changed

- finalize:
  - Migrate to Broker V2 package
  - Multiple completion messages may be published if finalize messages are consumed when file is already in ready
    - This is to address possible scenario where publish fails after commit during the setting of accession
  - Use db transactions to ensure correct state even if an error occurs.
- ingest:
  - Use db transactions during cancel and ingest actions and rollback if encounter error.
  - Requeue messages which could be expected to succeed on a retry.
  - Add a "error-queue-reason" header when sending messages to the error queue which can not be retried to record reason.
  - Update unit tests to use mocks of the db, and storage reader, writer instead of docker / temp directory.
    - Remove redundant unit tests which are covered by other tests, and remove unit tests which should be covered by integration tests.
  - Use log/slog for logging instead of logrus.
  - Wait for the handler that is running to finish on shutdown before closing the broker; a second signal skips the wait.
- Update remaining mocks to implement github.com/stretchr/testify/mock.Mock.
- database: add ErrUniqueViolation, ErrNotNullViolation, ErrForeignKeyViolation errors,
  - Add pq error code parsing for these in the Postgres implementation
- mapper: 
  - Update to use broker/v2 instead of broker (v1)
  - Add unit tests
- verify:
  - Update to use broker/v2 instead of broker (v1)
  - Add unit tests
- rotatekey:
  - Update to use broker/v2 instead of broker (v1)
  - Update unit tests to use mocks instead of docker 
  - Use transaction for write actions during message handling incase error for rollback
  - Remove dependency on config(v1) package, add required rotatekey related configuration to be registrated to config/v2
    - Changed
      - C4GH_ROTATEPUBKEYPATH -> TARGET_PUBLIC_KEY
      - GRPC_HOST + GRPC_PORT -> REENCRYPT_TARGET
      - GRPC_CACERT -> REENCRYPT_CA_CERT
      - GRPC_CLIENTCERT -> REENCRYPT_CLIENT_CERT
      - GRPC_CLIENTKEY -> REENCRYPT_CLIENT_KEY
      - GRPC_TIMEOUT -> REENCRYPT_TIMEOUT, also now a time.Duration instead of integer of seconds
      - BROKER_QUEUE -> SOURCE_QUEUE
      - BROKER_ROUTINGKEY -> ROUTING_KEY
      - BROKER_PREFETCHCOUNT -> BROKER_PREFETCH_COUNT
- intercept:
  - Update to use broker/v2 instead of broker (v1)
  - Refactor and add unit tests
  - Add configuration options for destination routing keys
- s3inbox:
  - Update to use broker/v2 instead of broker (v1)
  - Fix reuploads to publish the "remove" message before the "upload" messages, instead of after 
  - Update unit test to use mocks instead of docker containers
  - Add additional checks in detectS3RequestType, to reject additional s3 action not previously rejected but not supported
  - Remove dependency on config (v1) by moving required configuration registration to s3inbox/config
    - Env variable changes:
      - SERVER_JWTPUBKEYPATH -> SERVER_JWT_PUB_KEY_PATH
      - SERVER_JWTPUBKEYURL -> SERVER_JWT_PUB_KEY_URL
      - BROKER_ROUTING_KEY -> ROUTING_KEY
      - S3INBOX_CACERT -> S3INBOX_CA_CERT
- sync:
  - Update to use broker/v2 instead of broker (v1)
  - Add unit tests
  - Allow the remote configuration to be optional, and don't send http notifications if it is not configured
  - Do not hardcode the `/dataset` path when doing the http calls to the configured remote, instead rely on it being configured in the remote.url

### Fixed

- auth: report what the login provider refused. An OAuth 2 error redirect to `/oidc/login` carries an `error` instead of a `code`; the empty code was exchanged anyway and the user was told to clear their session cookies.
- Fixed downloading files by the `file_dataset.download_path` in the sda-download(v1) 
- broker/v2:
  - A handler that is already running when shutdown starts now gets `broker.shutdown_grace` (a duration, default `20s`) to finish and ack its message, instead of failing its publish with `context canceled`.
  - Cancel the consumer by its real tag on shutdown, without waiting for the server's reply; `Cancel("")` referred to a tag the client never registered, and waiting for the reply held the broker mutex while a slow or gone server never answered.
  - Do not start on a new delivery once shutdown has begun; the grace period is for the handler that was already running.
  - Do not reconnect after `Close()`, so a handler that outlives shutdown cannot publish a duplicate; `Close()` now takes the broker mutex.
  - `Alive()` makes one bounded reconnect attempt when the connection is down, so a service that only publishes recovers through its readiness probe instead of needing a restart; it answers within a few seconds and never waits behind another caller's dial.
  - `Publish` waits on a confirmation that belongs to that publish alone; on the shared `NotifyPublish` channel concurrent publishers, as in api, could get each other's ack or nack, and a confirmation nobody read within five seconds was dropped.
  - A reconnect attempt returns when its context is done even while the library is still in a handshake or channel open it cannot interrupt; the abandoned attempt closes what it opened.
  - The consumer tag is cut to fit AMQP's 255-byte limit for long queue names.
  - Reconnects are serialised on their own mutex and the dial no longer holds the broker state mutex, so `Close()` and `Alive()` are not blocked for the dial and handshake timeouts while a reconnect is in flight, two callers that both see a dead connection result in one dial, and the TCP dial and handshake follow the caller's context deadline.
  - `Close()` closes the connection with a deadline instead of closing each channel and waiting for the server's reply without one, so shutdown against a frozen server ends after a few seconds instead of the heartbeat timeout; a connection the server had already dropped is no longer reported as an error.
- s3 writer: don't panic or upload to an empty bucket name when every endpoint is full
- api: fix publishing to correct destination on POST /dataset/release/{datasetid}
- Populate slog log level from LOG_LEVEL configuration

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
