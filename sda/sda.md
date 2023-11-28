SDA - Sensitive Data Archive
============

Repository:
[neicnordic/sensitive-data-archive](https://github.com/neicnordic/sensitive-data-archive)

`sda` repository consists of a suite of services which are part of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/) and implements the components required for data submission.
It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as an isolated Sensitive Data Archive.
`sda` was built with support for both S3 and POSIX storage.

The SDA submission pipeline has four main steps:

1. [Ingest](cmd/ingest/ingest.md) splits file headers from files, moving the header to the database and the file content to the archive storage.
2. [Verify](cmd/verify/verify.md) verifies that the header is encrypted with the correct key, and that the checksums match the user-provided checksums.
3. [Finalize](cmd/finalize/finalize.md) associates a stable accessionID with each archive file and backups the file.
4. [Mapper](cmd/mapper/mapper.md) maps file accessionIDs to a datasetID.

There are also three additional support services:

1. [Intercept](cmd/intercept/intercept.md) relays messages from Central EGA to the system.
2. [s3inbox](cmd/s3inbox/s3inbox.md) proxies uploads to the an S3 compatible storage backend.
