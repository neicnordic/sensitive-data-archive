# sda-pipeline: ingest

Splits the Crypt4GH header and moves it to database. The remainder of the file
is sent to the storage backend (archive). No cryptographic tasks are done.

## Configuration

There are a number of options that can be set for the ingest service.
These settings can be set by mounting a yaml-file at `/config.yaml` with settings.

ex.
```yaml
log:
  level: "debug"
  format: "json"
```
They may also be set using environment variables like:
```bash
export LOG_LEVEL="debug"
export LOG_FORMAT="json"
```

### Keyfile settings

These settings control which crypt4gh keyfile is loaded.

 - `C4GH_FILEPATH`: filepath to the crypt4gh keyfile
 - `C4GH_PASSPHRASE`: pass phrase to unlock the keyfile

### RabbitMQ broker settings

These settings control how ingest connects to the RabbitMQ message broker.

 - `BROKER_HOST`: hostname of the rabbitmq server

 - `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)

 - `BROKER_QUEUE`: message queue to read messages from (commonly `ingest`)

 - `BROKER_ROUTINGKEY`: message queue to write success messages to (commonly `archived`)

 - `BROKER_USER`: username to connect to rabbitmq

 - `BROKER_PASSWORD`: password to connect to rabbitmq

 - `BROKER_PREFETCHCOUNT`: Number of messages to pull from the message server at the time (default to 2)

### PostgreSQL Database settings:

 - `DB_HOST`: hostname for the postgresql database

 - `DB_PORT`: database port (commonly 5432)

 - `DB_USER`: username for the database

 - `DB_PASSWORD`: password for the database

 - `DB_DATABASE`: database name

 - `DB_SSLMODE`: The TLS encryption policy to use for database connections.
   Valid options are:
    - `disable`
    - `allow`
    - `prefer`
    - `require`
    - `verify-ca`
    - `verify-full`

   More information is available
   [in the postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)

   Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set,
   and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set

 - `DB_CLIENTKEY`: key-file for the database client certificate

 - `DB_CLIENTCERT`: database client certificate file

 - `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### Storage settings

Storage backend is defined by the `ARCHIVE_TYPE`, and `INBOX_TYPE` variables.
Valid values for these options are `S3` or `POSIX`
(Defaults to `POSIX` on unknown values).

The value of these variables define what other variables are read.
The same variables are available for all storage types, differing by prefix (`ARCHIVE_`, or  `INBOX_`)

if `*_TYPE` is `S3` then the following variables are available:
 - `*_URL`: URL to the S3 system
 - `*_ACCESSKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
 - `*_SECRETKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
 - `*_BUCKET`: The S3 bucket to use as the storage root
 - `*_PORT`: S3 connection port (default: `443`)
 - `*_REGION`: S3 region (default: `us-east-1`)
 - `*_CHUNKSIZE`: S3 chunk size for multipart uploads.
# CA certificate is only needed if the S3 server has a certificate signed by a private entity
 - `*_CACERT`: Certificate Authority (CA) certificate for the storage system

and if `*_TYPE` is `POSIX`:
 - `*_LOCATION`: POSIX path to use as storage root

### Logging settings:

 - `LOG_FORMAT` can be set to “json” to get logs in json format.
   All other values result in text logging

 - `LOG_LEVEL` can be set to one of the following, in increasing order of severity:
    - `trace`
    - `debug`
    - `info`
    - `warn` (or `warning`)
    - `error`
    - `fatal`
    - `panic`

## Service Description
The ingest service copies files from the file inbox to the archive, and registers them in the database.

When running, ingest reads messages from the configured RabbitMQ queue (default: "ingest").
For each message, these steps are taken (if not otherwise noted, errors halt progress and the service moves on to the next message):

1.  The message is validated as valid JSON that matches the "ingestion-trigger" schema (defined in sda-common).
If the message can’t be validated it is discarded with an error message in the logs.

1. A file reader is created for the filepath in the message.
If the file reader can’t be created an error is written to the logs, the message is Nacked and forwarded to the error queue.

1. The file size is read from the file reader.
On error, the error is written to the logs, the message is Nacked and forwarded to the error queue.

1. A uuid is generated, and a file writer is created in the archive using the uuid as filename.
On error the error is written to the logs and the message is Nacked and then re-queued.

1. The filename is inserted into the database along with the user id of the uploading user. In case the file is already existing in the database, the status is updated.
Errors are written to the error log.
Errors writing the filename to the database do not halt ingestion progress.

1. The header is read from the file, and decrypted to ensure that it’s encrypted with the correct key.
If the decryption fails, an error is written to the error log, the message is Nacked, and the message is forwarded to the error queue.

1. The header is written to the database.
Errors are written to the error log.

1. The header is stripped from the file data, and the remaining file data is written to the archive.
Errors are written to the error log.

1. The size of the archived file is read.
Errors are written to the error log.

1. The database is updated with the file size, archive path, and archive checksum, and the file is set as “archived”.
Errors are written to the error log.
This error does not halt ingestion.

1. A message is sent back to the original RabbitMQ broker containing the upload user, upload file path, database file id, archive file path and checksum of the archived file.

## Communication

 - Ingest reads messages from one rabbitmq queue (commonly `ingest`).

 - Ingest writes messages to one rabbitmq queue (commonly `archived`).

 - Ingest inserts file information in the database using three database functions, `InsertFile`, `StoreHeader`, and `SetArchived`.

 - Ingest reads file data from inbox storage and writes data to archive storage.
