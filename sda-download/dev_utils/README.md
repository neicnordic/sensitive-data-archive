# Local testing howto

## Getting started locally
In root repository

```
export CONFIGFILE="./dev_utils/config.yaml"
go run cmd/main.go
```

This requires having the certificates generated and the database up.

First create the necessary credentials.

```command
sh make_certs.sh
```

## Getting up and running fast with docker compose

```command
docker-compose -f compose-no-tls.yml up -d
```

For testing the API

```command
sh run_integration_test_no_tls.sh
```

## Starting the services using docker compose with TLS enabled

To start all the backend services using docker compose.

```command
docker compose up -d db s3 mockauth
```

To start all the sda services using docker compose.

```command
docker compose up -d
```

To see brief real-time logs at the terminal remove the -d flag.

For testing the API

```command
sh run_integration_test.sh
```

## Manually run the integration test

For step-by-step tests follow instructions below.

### Create database entry

Entries need to be created in 2 tables:
1. `local_ega.main` for information on the encrypted file, including its header
2. `local_ega_ebi.filedataset` for the file to dataset mapping

See more information on how these are generated in [sda-pipeline](https://github.com/neicnordic/sda-pipeline) file ingestion microservices.

```
docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "INSERT INTO local_ega.main (id, stable_id, status, submission_file_path, submission_file_extension, submission_file_calculated_checksum, submission_file_calculated_checksum_type, submission_file_size, submission_user, archive_file_reference, archive_file_type, archive_file_size, archive_file_checksum, archive_file_checksum_type, decrypted_file_size, decrypted_file_checksum, decrypted_file_checksum_type, encryption_method, version, header, created_by, last_modified_by, created_at, last_modified) VALUES (1, 'urn:neic:001-002', 'READY', 'dummy_data.c4gh', 'c4gh', '5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578', 'SHA256', NULL, 'test', '4293c9a7-dc50-46db-b79a-27ddc0dad1c6', NULL, 1049081, '74820dbcf9d30f8ccd1ea59c17d5ec8a714aabc065ae04e46ad82fcf300a731e', 'SHA256', 1048605, '06bb0a514b26497b4b41b30c547ad51d059d57fb7523eb3763cfc82fdb4d8fb7', 'SHA256', 'CRYPT4GH', NULL, '637279707434676801000000010000006c000000000000006af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc5073799e207ec5d022b2601340585ff082565e55fbff5b6cdbbbe6b12a0d0a19ef325a219f8b62344325e22c8d26a8e82e45f053f4dcee10c0ec4bb9e466d5253f139dcd4be', 'lega_in', 'lega_in', '2021-12-13 16:18:06.512169+00', '2021-12-13 16:18:06.512169+00');"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "INSERT INTO local_ega_ebi.filedataset (id, file_id, dataset_stable_id) VALUES (1, 1, 'https://doi.example/ty009.sfrrss/600.45asasga');"

```

### Upload file to the archive

Upload the dummy datafile to the s3 archive.

```cmd
s3cmd -c s3cmd.conf put archive_data/4293c9a7-dc50-46db-b79a-27ddc0dad1c6 s3://archive/4293c9a7-dc50-46db-b79a-27ddc0dad1c6
```

Browse the s3 buckets at:

```http
https://localhost:9000
```

### Get a token

The mockauth service provides tokens that contain already permissions for the dataset inserted in the db.

`token=$(curl --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r '.[0]')` - first token of this response is a good token, the 2nd one is a token without any permissions and the 3rd one is a token to be tested when using a trusted source


### Try to query the endpoint

The [API Reference](../docs/API.md) has example requests and responses.

For quick example run: `curl --cacert certs/ca.pem -H "Authorization: Bearer $token" https://localhost:8443/metadata/datasets`
