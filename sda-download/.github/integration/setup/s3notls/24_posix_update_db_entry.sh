#!/bin/bash

docker run --rm --name client --network dev_utils_default \
    -v "dev_utils_certs:/certs" \
    -e PGSSLCERT=/certs/client.pem \
    -e PGSSLKEY=/certs/client-key.pem \
    -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest \
    postgresql://postgres:rootpassword@db:5432/sda \
    -t -q -c "UPDATE sda.files SET archive_location = 'http://s3:9000/archive';"