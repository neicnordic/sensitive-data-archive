# sync-api

The sync-api service is used in the [Bigpicture](https://bigpicture.eu/) project.

## Service Description

The sync service facilitates replication of data and metadata between the nodes in the consortium.

When enabled the service will perform the following tasks:

1. Upon receiving a POST request with JSON data to the `/dataset` route.
   1. Parse the JSON blob and validate it against the `file-sync` schema.
   2. Build and send messages to start ingestion of files.
   3. Build and send messages to assign stableIDs to files.
   4. Build and send messages to map files to a dataset.

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

### Service settings

- `SYNC_API_PASSWORD`: password for the API user
- `SYNC_API_USER`: User that will be allowed to send POST requests to the API

### RabbitMQ broker settings

These settings control how sync connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the rabbitmq server
- `BROKER_PORT`: rabbitmq broker port (commonly `5671` with TLS and `5672` without)
- `BROKER_EXCHANGE`: exchange to send messages to
- `BROKER_USER`: username to connect to rabbitmq
- `BROKER_PASSWORD`: password to connect to rabbitmq
- `BROKER_PREFETCHCOUNT`: Number of messages to pull from the message server at the time (default to 2)

The default routing keys for sending ingestion, accession and mapping messages can be overridden by setting the following values:

- `SYNC_API_ACCESSIONROUTING`
- `SYNC_API_INGESTROUTING`
- `SYNC_API_MAPPINGROUTING`

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