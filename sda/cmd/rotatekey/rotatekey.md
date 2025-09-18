# rotatekey Service

Rotates the crypt4gh encryption key of a file that is mapped to a dataset and stored in the SDA.

## Service Description

The rotatekey service re-encrypts the header of a file with the configured target key, and updates the database with the new header and encryption key hash.

When running, rotatekey reads messages from the `rotatekey_stream` RabbitMQ queue.
For each message, these steps are taken (errors halts progress, the message is Nack'ed, an info-error message is sent and the service moves on to the next message):

1. The message is validated as valid JSON that matches the "dataset-mapping" schema.
2. A database look-up is performed for the configured target public key hash. If the look-up fails or the key has been deprecated, an error is raised.
3. If the massage contains more than one accession IDs, an error is raised and the message is discarded as the service works on a one file per message basis.
4. The file ID is fetched from the database.
5. The key hash of the c4gh key with which the file is currently encrypted is fetched from the database and compared with the configured target key.
6. If these key hashes differ, the reencrypt service is called to re-encrypt the file header with the target key.
7. The file header entry in the database is updated with the new one.
8. The key hash entry for the database is updated with the new one (target key).
9. A re-verify message is compiled, validated and sent to the archived queue so that it is consumed by verify service.
10. The message is Ack'ed.

## Communication

- Rotatekey reads messages from one rabbitmq stream (`rotatekey_stream`)
- Rotatekey reads file information, headers and key hashes from the database and can not be started without a database connection.
- Rotatekey makes grpc calls to reencrypt service for re-encrypting the header with the target public key.
- Rotatekey sends messages to the `archived` queue for consumption by the verify service.

## Configuration

There are a number of options that can be set for the rotatekey service.
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

### Public Key file settings

This setting controls which crypt4gh keyfile is loaded.

- `C4GH_ROTATEPUBKEYPATH`: path to the crypt4gh public key to use for reencrypting file headers.

### RabbitMQ broker settings

These settings control how sync connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the rabbitmq server
- `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queue or stream to read messages from (commonly `rotatekey_stream`)
- `BROKER_USER`: username to connect to rabbitmq
- `BROKER_PASSWORD`: password to connect to rabbitmq
- `BROKER_ROUTINGKEY`: routing from a rabbitmq exchange to the rotatekey queue

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

  More information is available
  [in the postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)

  Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set,
  and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set.

- `DB_CLIENTKEY`: key-file for the database client certificate
- `DB_CLIENTCERT`: database client certificate file
- `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### GRPC settings

- `GRPC_HOST`: Host name of the grpc server
- `GRPC_PORT`: Port number of the grpc server
- `GRPC_CACERT`: Certificate Authority (CA) certificate for validating incoming request
- `GRPC_SERVERCERT`: path to the x509 certificate used by the service
- `GRPC_SERVERKEY`: path to the x509 private key used by the service


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
