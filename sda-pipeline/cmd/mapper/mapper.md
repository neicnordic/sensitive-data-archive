# sda-pipeline: mapper

The mapper service registers mapping of accessionIDs (stable ids for files) to datasetIDs.

## Configuration

There are a number of options that can be set for the mapper service.
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

### RabbitMQ broker settings

These settings control how mapper connects to the RabbitMQ message broker.

 - `BROKER_HOST`: hostname of the rabbitmq server

 - `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)

 - `BROKER_QUEUE`: message queue to read messages from (commonly `mapper`)

 - `BROKER_USER`: username to connect to rabbitmq

 - `BROKER_PASSWORD`: password to connect to rabbitmq

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

The mapper service maps file accessionIDs to datasetIDs.

When running, mapper reads messages from the configured RabbitMQ queue (default: "mappings").
For each message, these steps are taken (if not otherwise noted, errors halt progress and the service moves on to the next message):

1. The message is validated as valid JSON that matches the "dataset-mapping" schema (defined in sda-common).
If the message can’t be validated it is discarded with an error message in the logs.

1. AccessionIDs from the message are mapped to a datasetID (also in the message) in the database.
On error the service sleeps for up to 5 minutes to allow for database recovery, after 5 minutes the message is Nacked, re-queued and an error message is written to the logs.

1. The RabbitMQ message is Ack'ed.


## Communication

 - Mapper reads messages from one rabbitmq queue (default `mappings`).

 - Mapper maps files to datasets in the database using the `MapFilesToDataset` function.
