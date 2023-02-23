# sda-pipeline: intercept

The intercept service relays messages between Central EGA and Federated EGA nodes.

## Configuration

There are a number of options that can be set for the intercept service.
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

These settings control how intercept connects to the RabbitMQ message broker.

 - `BROKER_HOST`: hostname of the rabbitmq server

 - `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)

 - `BROKER_QUEUE`: message queue to read messages from (commonly `files`)

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

When running, intercept reads messages from the configured RabbitMQ queue (default: "files").
For each message, these steps are taken (if not otherwise noted, errors halt progress, the message is Nack'ed, the error is written to the log, and to the rabbitMQ error queue.
Then the service moves on to the next message):

1. The message type is read from the message "type" field.

1. The message schema is read from the message "msgType" field.

1. The message is validated as valid JSON following the schema read in the previous step.
If this fails an error is written to the logs, but not to the error queue and the message is not Ack'ed or Nack'ed.

1. The correct queue for the message is decided based on message type.
This is not supposed to be able to fail.

1. The message is re-sent to the correct queue.
This has no error handling as the resend-mechanism hasn't been finished.

1. The message is Ack'ed.

## Communication

 - Intercept reads messages from one rabbitmq queue (default `files`).

 - Intercept writes messages to three rabbitmq queues, `accessionIDs`, `ingest`, and `mappings`.
