# Changelog - sda-svc Helm Chart

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [4.0.0] - 2026-06-03

### Changed

- Replace flat scalar storage keys with nested `s3:` and `posix:` lists
  on `global.archive`, `global.backupArchive`, `global.inbox`, and
  `global.sync.destination`, unlocking multi-endpoint storage
  configurations.
- Move quota fields (`maxBuckets`, `maxObjects`, `maxSize`) into each
  list entry. `locationBrokerCacheTTL` stays at backend level but is
  now rendered at config-file root (fixes pre-existing bug where the
  service silently ignored the value).
- Move POSIX k8s volume backing into each posix list entry under a
  `volume:` block. Pod volumes renamed: `archive` → `archive-0`,
  `backup` → `backup-archive-0` (note lowercase), `inbox` → `inbox-0`.
- DOA (legacy Java service) reads index 0 of the new lists.

### Added

- Multi-endpoint storage support for S3 and POSIX backends.
- POSIX `sync-dest-0` volume for sync destinations.
- DOA multi-endpoint fail-fast guard: `doa.enabled: true` with more
  than one archive endpoint fails at render time.

### Removed

- `storageType` selector. Rendering is driven by list presence.

See `MIGRATION-4.0.md` for upgrade instructions and examples.

Closes #2387.

## [3.4.3] - 2026-05-29

### Changed

- Bump default appVersion -> v3.1.72

## [3.4.2] - 2026-05-25
- Started keeping a changelog after this version.
