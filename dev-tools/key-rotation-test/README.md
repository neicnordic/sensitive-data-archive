# Key Rotation Test Setup

This directory contains a demo script for testing the key rotation functionality of the Sensitive Data Archive (SDA).

The test covers the key rotation scenarios described in [Issue #2039](https://github.com/neicnordic/sensitive-data-archive/issues/2039).

## Prerequisites

Before running the test suite, ensure you have the following tools installed and available in your `PATH`:

*   **`sda-cli`**: Sensitive Data Archive CLI tool ([Installation / Build instructions](../../README.md#building-sda-cli))
*   **`s3cmd`**: S3 command-line client used for dataset upload/verification (`brew install s3cmd` or `apt install s3cmd`)
*   **`jq`**: Command-line JSON processor (`brew install jq` or `apt install jq`)
*   **`nc` (netcat)**: Networking utility used for service health checks (`apt install netcat-openbsd` or pre-installed on macOS)
*   **`docker` & `docker compose`**: Container runtime to execute the services and tests

## Quick start

### 1. Build the Docker images and start the development environment

From the root of the repository, build required images and start all
services for the key rotation tests:

```bash
make dev-key-rotation-up
```

Wait until all services are up and healthy before continuing.

### 2. Run the key rotation demo

From the `dev-tools/key-rotation-test` directory, run:

```bash
bash demo.sh
```

The script runs the test cases sequentially. It pauses after each test case and waits for you to press Enter before starting the next one.

You can also run a single test case by passing its number (1-9) as an argument, for example:

```bash
bash demo.sh 3
```

## What the test does

The demo script automatically:

* Creates a clean PostgreSQL database snapshot before running the tests.
* Copies the shared test credentials from the `verify` container to `/tmp/sda/shared`.
* Obtains an authentication token from the local token service.
* Runs all 9 key rotation test cases sequentially.
* Restores the database to the clean snapshot between test cases when required.
* Creates temporary test data under `/tmp/sda/test-data`.
* Verifies the results by downloading and decrypting files, checking database state, and validating HTTP responses.

The database snapshot is created at the beginning of the test run and is used by the script to restore the database to a clean baseline between scenarios.

## Test cases

The demo contains the following 9 test cases:

### Case 1 — Standard key lifecycle

Tests the normal key rotation lifecycle:

1. Download and decrypt a file using the current key.
2. Rotate the file to the new key.
3. Remove the old key from the `reencrypt` and `download` services.
4. Download and decrypt the rotated file.
5. Verify that an unrotated file can no longer be downloaded when its encryption key has been removed.

This verifies that files successfully migrated to the new key remain accessible after the old key is removed.

### Case 2 — Partial dataset rotation / mixed key state

Tests a dataset containing files encrypted with different keys.

Half of the files are rotated while the rest of files remain encrypted with the original key. The test then verifies that both the rotated and unrotated files can still be downloaded and decrypted while both keys are available.

### Case 3 — Rotation without a valid target key

Tests the behaviour when no valid target key is configured for rotation.

The test records the file's `key_hash`, attempts a rotation, and verifies that the database value remains unchanged after the rotation fails.

### Case 4 — Concurrent downloads during key rotation

Tests concurrent access while a file is being re-encrypted.

The script starts 10 parallel download workers, triggers key rotation while downloads are in progress, and then attempts to decrypt every downloaded file. The test passes only if none of the downloaded files are corrupted.

### Case 5 — Mixed-key ingestion during bulk rotation

Tests ingestion of files encrypted with different keys while a bulk dataset key rotation is running in the background.

The test uploads files encrypted with the old and new keys, ingests them while the bulk rotation is active, maps them to a dataset, and finally verifies that both files can be downloaded and decrypted successfully.

### Case 6 — Multi-key archive migration

Tests support for multiple encryption keys and migration of an archive containing files encrypted with five different keys.

The test registers four additional keys, encrypts test files with the five keys, ingests and archives them, and then performs a dataset-level key rotation.

### Case 7 — Reject ingestion with a deprecated key

Tests that files encrypted with a deprecated encryption key cannot be ingested.

> As of 2026-08-13, ingestion succeeds even when the file is encrypted with a deprecated key.

### Case 8 — Reject rotation to a deprecated key

Tests that key rotation cannot target an encryption key that has already been deprecated.

### Case 9 — Rotate from a deprecated source key

Tests that a file encrypted with a deprecated source key can still be migrated to a valid target key.

The test marks the source key as deprecated, triggers file rotation, verifies that the file's `key_hash` changes to the new key, and confirms that the resulting file can still be downloaded and decrypted.

## Test data and credentials

The script uses the following temporary directories:

```text
/tmp/sda/shared
/tmp/sda/test-data
```

`/tmp/sda/shared` contains the test credentials and key material copied from the `verify` container. `/tmp/sda/test-data` is used for downloaded files and other temporary test data and is cleaned at the beginning of the test run.

The script obtains the client authentication token from:

```text
http://localhost:8000/tokens
```

and uses the SDA API at:

```text
http://localhost:8090
```

The download endpoint used by the tests is exposed on port `8085`.

## Running individual test cases

By default, `demo.sh` runs all nine cases in sequence. Each case is implemented as a separate Bash function:

```text
case_1_standard_lifecycle
case_2_mixed_key_dataset
case_3_invalid_rotation_target
case_4_concurrent_race_condition
case_5_ingest_mixed_keys_during_rotation
case_6_multi_key_archive_migration
case_7_deprecated_key_ingest_rejection
case_8_rotate_to_deprecated_key_rejection
case_9_rotate_from_deprecated_source_key
```

The script currently invokes all nine functions directly, with a pause between each case.

## Expected result

A successful run should report `SUCCESS` for each test case and complete all nine scenarios without exiting early.

If a test fails, inspect the output from the corresponding step and, where applicable, the Docker Compose service logs. For example:

```bash
docker compose logs --tail=100 rotatekey
docker compose logs --tail=100 reencrypt
docker compose logs --tail=100 download
```

## Cleanup

The test script stores its temporary files under `/tmp/sda/test-data`. The test environment itself can be stopped using the corresponding development environment cleanup command after the test run is complete.

Because the test modifies the local Docker Compose environment and PostgreSQL database, it is recommended to use the dedicated key rotation development environment rather than an existing development instance containing data you want to preserve.
