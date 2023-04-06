# Local testing howto

## Getting up and running fast

```command
docker-compose -f compose-no-tls.yml up -d
```

For a complete test of the pipeline

```command
sh run_integration_test_no_tls.sh
```

## Starting the services using docker compose with TLS enabled

First create the necessary credentials.

```command
sh make_certs.sh
```

To start all the backend services using docker compose.

```command
docker-compose -f compose-sda.yml up -d db mq s3
```

To start all the sda services using docker compose.

```command
docker-compose -f compose-sda.yml up -d
```

To start one of the sda services using docker compose.

```command
docker-compose -f compose-sda.yml up -d ingest
```

To see brief real-time logs at the terminal remove the -d flag.

For a complete test of the pipeline

```command
sh run_integration_test.sh
```

## Starting the services in standalone mode

In this case, the `orchestrator` service is used in place of the `intercept` service.

Create the necessary credentials.

```command
sh make_certs.sh
```

Start stack by running,

```command
docker-compose -f compose-sda-standalone.yml up -d
```
or, to rebuild the images,
```command
docker-compose -f compose-sda-standalone.yml up --build -d
```

To see brief real-time logs at the terminal remove the `-d` option.

For a complete test of the ingestion cycle in standalone mode

```command
sh run_integration_test_standalone.sh
```

## Manually run the integration test

For step-by-step tests follow instructions below.

### Upload file to the inbox

Upload the dummy datafile to the s3 inbox under the folder /test.

```cmd
s3cmd -c s3cmd.conf put "dummy_data.c4gh" s3://inbox/test/dummy_data.c4gh
```

Browse the s3 buckets at:

```http
https://localhost:9000
```

### json formatted messages

In order to start the ingestion of the dummy datafile a message needs to be published to the `files` routing key of the `sda` exchange either via the API or the webui.

```command
curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' -H 'Content-Type: application/json;charset=UTF-8' --data-binary '{"vhost":"test","name":"sda","properties":{"delivery_mode":2,"correlation_id":"1","content_encoding":"UTF-8","content_type":"application/json"},"routing_key":"files","payload":"{\"type\": \"ingest\", \"user\": \"test\", \"filepath\": \"test/dummy_data.c4gh\", \"encrypted_checksums\":[{\"type\":\"sha256\", \"value\":\"5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578\", \"type\": \"md5\", \"value\": \"b60fa2486b121bed8d566bacec987e0d\"}]}","payload_encoding":"string"}'
```

More examples using the API and relevant message properties can be found in the integration test script.

Alternatively, to access the webui go to:

```http
https://localhost:15672
```

#### Message to start ingestion

```json
{
    "type": "ingest",
    "user": "test",
    "filepath": "test/dummy_data.c4gh",
    "encrypted_checksums": [
        { "type": "sha256", "value": "5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578" },
        { "type": "md5", "value": "b60fa2486b121bed8d566bacec987e0d" }
    ]
}
```

#### Example message to start verification

This step is automatically triggered by ingestion when all needed services are running. To initiate the verification of the dummy datafile manually, a message needs to be published to the `files` routing key of the `sda` exchange.

```json
{
    "user": "test",
    "filepath": "test/dummy_data.c4gh",
    "file_id": "1",
    "archive_path": "123e4567-e89b-12d3-a456-426614174000",
    "encrypted_checksums": [
        { "type": "sha256", "value": "5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578" },
        { "type": "md5", "value": "b60fa2486b121bed8d566bacec987e0d" }
    ]
}
```

The value of the archive path can be found by getting from the queue the message that was published when the header-stripped datafile is archived either by using the API or the webgui. This value corresponds to the name of the header-stripped file that is created in the archive bucket.

#### Example message to finalize ingestion

To finalize ingestion of the dummy datafile a message needs to be published to the `files` routing key of the `sda` exchange.

```json
{
    "type": "accession",
    "user": "test",
    "filepath": "test/dummy_data.c4gh",
    "accession_id": "EGAF00123456789",
    "decrypted_checksums": [
        { "type": "sha256", "value": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" },
        { "type": "md5", "value": "d41d8cd98f00b204e9800998ecf8427e" }
    ]
}
```

The values of the decrypted datafile checksums can be found by getting from the queue the message that was published at verification either by using the API or the webgui. After ingestion is finalized the backup bucket is backuped with the archive and contains the header-stripped datafile.

#### Example message to perform mapping

To register the mapping of the datafile IDs to the database a message needs to be published to the `files` routing key of the `sda` exchange.

```json
{
    "type": "mapping",
    "dataset_id": "EGAD00123456789",
    "accession_ids": ["EGAF00123456789"
    ]
}
```
