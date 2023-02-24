sda-pipeline
============

Repository:
[neicnordic/sda-pipeline](https://github.com/neicnordic/sda-pipeline/)

`sda-pipeline` is part of [NeIC Sensitive Data Archive](https://neic-sda.readthedocs.io/en/latest/) and implements the components required for data submission.
It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as an isolated Sensitive Data Archive.
`sda-pipeline` was built with support for both S3 and POSIX storage.

The SDA pipeline has four main steps:

1. [Ingest](ingest.md) splits file headers from files, moving the header to the database and the file content to the archive storage.
1. [Verify](verify.md) verifies that the header is encrypted with the correct key, and that the checksums match the user-provided checksums.
1. [Finalize](finalize.md) associates a stable accessionID with each archive file.
1. [Mapper](mapper.md) maps file accessionIDs to a datasetID.

There are also three additional support services:

1. [Backup](backup.md) copies data from archive storage to backup storage, optionally re-encrypting and re-attaching the headers.
1. [Intercept](intercept.md) relays messages from Central EGA to the system.
1. [Notify](notify.md) sends e-mail messages to users.

