# Sync

Copies files from the archive to the sync destination, including the header so that the files can be ingested at the remote site.

## Configuration

There are a number of options that can be set for the sync service.
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

- `C4GH_FILEPATH`: path to the crypt4gh keyfile
- `C4GH_PASSPHRASE`: pass phrase to unlock the keyfile
- `C4GH_SYNCPUBKEYPATH`: path to the crypt4gh public key to use for reencrypting file headers.

### RabbitMQ broker settings

These settings control how sync connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the rabbitmq server
- `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queueor stream to read messages from (commonly `completed_stream`)
- `BROKER_USER`: username to connect to rabbitmq
- `BROKER_PASSWORD`: password to connect to rabbitmq
- `BROKER_PREFETCHCOUNT`: Number of messages to pull from the message server at the time (default to 2)

### PostgreSQL Database settings

- `DB_HOST`: hostname for the postgresql database
- `DB_PORT`: database port (commonly 5432)
- `DB_USER`: username for the database
- `DB_PASSWORD`: password for the database
- `DB_DATABASE`: database name
- `DB_SSLMODE`: The TLS encryption policy to use for database connections. Valid options are:
  - `disable`
  - `allow`
  - `prefer`
  - `require`
  - `verify-ca`
  - `verify-full`

   More information is available [in the postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)

Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set, and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set

- `DB_CLIENTKEY`: key-file for the database client certificate
- `DB_CLIENTCERT`: database client certificate file
- `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### Storage settings

Storage backend is defined by the `ARCHIVE_TYPE`, and `SYNC_DESTINATION_TYPE` variables.
Valid values for these options are `S3` or `POSIX` for `ARCHIVE_TYPE` and `POSIX`, `S3` or `SFTP` for `SYNC_DESTINATION_TYPE`.

The value of these variables define what other variables are read.
The same variables are available for all storage types, differing by prefix (`ARCHIVE_`, or  `SYNC_DESTINATION_`)

if `*_TYPE` is `S3` then the following variables are available:

- `*_URL`: URL to the S3 system
- `*_ACCESSKEY`: The S3 access and secret key are used to authenticate to S3, [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `*_SECRETKEY`: The S3 access and secret key are used to authenticate to S3, [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `*_BUCKET`: The S3 bucket to use as the storage root
- `*_PORT`: S3 connection port (default: `443`)
- `*_REGION`: S3 region (default: `us-east-1`)
- `*_CHUNKSIZE`: S3 chunk size for multipart uploads.
- `*_CACERT`: Certificate Authority (CA) certificate for the storage system, CA certificate is only needed if the S3 server has a certificate signed by a private entity

if `*_TYPE` is `POSIX`:

- `*_LOCATION`: POSIX path to use as storage root

and if `*_TYPE` is `SFTP`:

- `*_HOST`: URL to the SFTP server
- `*_PORT`: Port of the SFTP server to connect to
- `*_USERNAME`: Username connectin to the SFTP server
- `*_HOSTKEY`: The SFTP server's public key
- `*_PEMKEYPATH`: Path to the ssh private key used to connect to the SFTP server
- `*_PEMKEYPASS`: Passphrase for the ssh private key

### Logging settings

- `LOG_FORMAT` can be set to “json” to get logs in json format. All other values result in text logging
- `LOG_LEVEL` can be set to one of the following, in increasing order of severity:
  - `trace`
  - `debug`
  - `info`
  - `warn` (or `warning`)
  - `error`
  - `fatal`
  - `panic`

## Service Description

The sync service copies files from the archive storage to sync storage.

When running, sync reads messages from the "completed" RabbitMQ queue.  
For each message, these steps are taken (if not otherwise noted, errors halts progress, the message is Nack'ed, and the service moves on to the next message):

1. The message is validated as valid JSON that matches the "ingestion-completion" schema. If the message can’t be validated it is sent to the error queue for later analysis.
2. The archive file path and file size is fetched from the database.
3. The file size on disk is requested from the storage system.
4. The archive file size from the database is compared against the disk file size.
5. A file reader is created for the archive storage file, and a file writer is created for the sync storage file.
   1. The header is read from the database.
   2. The header is decrypted.
   3. The header is reencrypted with the destinations public key.
   4. The header is written to the sync file writer.
6. The file data is copied from the archive file reader to the sync file writer.
7. The message is Ack'ed.

## Communication

- Sync reads messages from one rabbitmq stream (`completed_stream`)
- Sync reads file information and headers from the database and can not be started without a database connection. This is done using the `GetArchived`, and `GetHeaderForStableID` functions.
- Sync reads data from archive storage and writes data to sync destination storage.
