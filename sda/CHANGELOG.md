# Changelog - Sensitive Data Archive

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

### Changed

- Updated the sda-api `dataset/create` API to not reject requests if the requested files belong to different users

## [3.1.72] - 2026-05-29

### Fixed

- Fixed Unhandled error linter issues in sda/download

## [3.1.71] - 2026-05-25
- Started keeping a changelog after this version.