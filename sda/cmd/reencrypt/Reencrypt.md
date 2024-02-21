# reEncrypt service

Reencrypts a given file header with a given crypt4gh public key.

## Service Description

The `reencrypt` service uses the gRPC protocol for communication.

It receives the header to be encrypted as a byte array and the publickey as a base64 encoded string and returns the new header as a byte array.

## Configuration

There are a number of options that can be set for the `reencrypt` service.
These settings can be set by mounting a yaml-file at `/config.yaml` with settings.

ex.

```yaml
c4gh:
    filepath: "path/to/crypt4gh/file"
    passphrase: "passphrase to unlock the keyfile"
grpc:
    cacert: "path to (CA) certificate file for validating incoming request"
    serverkey: "path to the x509 certificate used by the service"
    servercert: "path to the x509 private key used by the service"
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
- `C4GH_PASSPHRASE`: passphrase to unlock the keyfile

### Logging settings

- `LOG_FORMAT` can be set to `json` to get logs in JSON format. All other values result in text logging.
- `LOG_LEVEL` can be set to one of the following, in increasing order of severity:
  - `trace`
  - `debug`
  - `info`
  - `warn` (or `warning`)
  - `error`
  - `fatal`
  - `panic`

### TLS settings

- `GRPC_CACERT`: Certificate Authority (CA) certificate for validating incoming request
- `GRPC_SERVERCERT`: path to the x509 certificate used by the service
- `GRPC_SERVERKEY`: path to the x509 private key used by the service
