# s3inbox Service

The `s3inbox` proxies uploads to the an S3 compatible storage backend. Users are authenticated with a JWT instead of `access_key` and `secret_key` used normally for `S3`.

## Service Description

The `s3inbox` proxies uploads to an S3 compatible storage backend.

1. Parses and validates the JWT token (`access_token` in the S3 config file) against the public keys, either locally provisioned or from OIDC JWK endpoints.
2. If the token is valid the file is passed on to the S3 backend
3. The file is registered in the database
4. The `inbox-upload` message is sent to the `inbox` queue, with the `sub` field from the token as the `user` in the message. If this fails an error will be written to the logs.

## Communication

- `s3inbox` proxies uploads to inbox storage.
- `s3inbox` inserts file information in the database using the `RegisterFile` database function and marks it as uploaded in the `file_event_log`
- `s3inbox` writes messages to one RabbitMQ queue (commonly: `inbox`).

## Configuration

There are a number of options that can be set for the `s3inbox` service.
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

### Server settings

These settings control the TLS status and where the service gets the public keys to validate the JWT tokens.

- `SERVER_CERT`: path to the x509 certificate used by the service
- `SERVER_KEY`: path to the x509 private key used by the service
- `SERVER_JWTPUBKEYPATH`: full path to the folder containing public keys used to validate JWT tokens
- `SERVER_JWTPUBKEYURL`: URL to OIDC JWK endpoint

### RabbitMQ broker settings

These settings control how verify connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the RabbitMQ server
- `BROKER_PORT`: RabbitMQ broker port (commonly: `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queue to read messages from (commonly: `archived`)
- `BROKER_ROUTINGKEY`: Routing key for publishing messages (commonly: `verified`)
- `BROKER_USER`: username to connect to RabbitMQ
- `BROKER_PASSWORD`: password to connect to RabbitMQ
- `BROKER_PREFETCHCOUNT`: Number of messages to pull from the message server at the time (default to `2`)

### PostgreSQL Database settings

- `DB_HOST`: hostname for the postgresql database
- `DB_PORT`: database port (commonly: `5432`)
- `DB_PASSWORD`: password for the database
- `DB_DATABASE`: database name
- `DB_SSLMODE`: The TLS encryption policy to use for database connections, valid options are:
    - `disable`
    - `allow`
    - `prefer`
    - `require`
    - `verify-ca`
    - `verify-full`

  More information is available
  [in the postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)  

  Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set, and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set.

- `DB_CLIENTKEY`: key-file for the database client certificate
- `DB_CLIENTCERT`: database client certificate file
- `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### Storage settings

- `INBOX_TYPE`: Valid value is `S3`
- `INBOX_URL`: URL to the S3 system
- `INBOX_ACCESSKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `INBOX_SECRETKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `INBOX_BUCKET`: The S3 bucket to use as the storage root
- `INBOX_PORT`: S3 connection port (default: `443`)
- `INBOX_REGION`: S3 region (default: `us-east-1`)
- `INBOX_CHUNKSIZE`: S3 chunk size for multipart uploads.
- `INBOX_CACERT`: Certificate Authority (CA) certificate for the storage system, this is only needed if the S3 server has a certificate signed by a private entity

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
