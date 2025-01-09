#!/bin/bash
set -e

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

# insert file entry into database
file_id=$(docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.files (stable_id, submission_user, \
        submission_file_path, submission_file_size, archive_file_path, \
        archive_file_size, decrypted_file_size, backup_path, header, \
        encryption_method) VALUES ('urn:neic:001-002', 'integration-test', 'dummy_data.c4gh', \
        1048729, '4293c9a7-dc50-46db-b79a-27ddc0dad1c6', 1049081, 1048605, \
        '', '637279707434676801000000010000006c000000000000006af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc5073799e207ec5d022b2601340585ff082565e55fbff5b6cdbbbe6b12a0d0a19ef325a219f8b62344325e22c8d26a8e82e45f053f4dcee10c0ec4bb9e466d5253f139dcd4be', 'CRYPT4GH') RETURNING id;" | xargs)

# insert "ready" database log event
docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.file_event_log (file_id, event) \
        VALUES ('$file_id', 'ready');"

docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '06bb0a514b26497b4b41b30c547ad51d059d57fb7523eb3763cfc82fdb4d8fb7', 'SHA256', 'UNENCRYPTED');"

docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578', 'SHA256', 'UPLOADED');"

docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.checksums (file_id, checksum, type, source) \
        VALUES ('$file_id', '74820dbcf9d30f8ccd1ea59c17d5ec8a714aabc065ae04e46ad82fcf300a731e', 'SHA256', 'ARCHIVED');"


# make sure that the dataset exists in the database
dataset_id=$(docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.datasets (stable_id) VALUES ('https://doi.example/ty009.sfrrss/600.45asasga') \
        ON CONFLICT (stable_id) DO UPDATE \
        SET stable_id=excluded.stable_id RETURNING id;")

# insert the file into the dataset
docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/lega \
    -t -q -c "INSERT INTO sda.file_dataset (file_id, dataset_id) \
        VALUES ('$file_id', $dataset_id);"
