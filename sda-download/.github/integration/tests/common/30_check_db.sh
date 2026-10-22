#!/bin/bash
set -e

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	ghcr.io/neicnordic/sda-ci-mirror:neicnordic-pg-client-sha-52f4bc3ba309 postgresql://postgres:rootpassword@db:5432/sda \
	-t -c "SELECT * from sda.file_dataset"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	ghcr.io/neicnordic/sda-ci-mirror:neicnordic-pg-client-sha-52f4bc3ba309 postgresql://postgres:rootpassword@db:5432/sda \
	-t -c "SELECT * from sda.file_event_log"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	ghcr.io/neicnordic/sda-ci-mirror:neicnordic-pg-client-sha-52f4bc3ba309 postgresql://postgres:rootpassword@db:5432/sda \
	-t -c "SELECT id, stable_id, archive_file_path, archive_location FROM sda.files ORDER BY id DESC"
