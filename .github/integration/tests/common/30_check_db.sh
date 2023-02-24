#!/bin/bash

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.file_dataset"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.filedataset ORDER BY id DESC"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT id, status, stable_id, archive_path FROM local_ega.files ORDER BY id DESC"
