#!/bin/sh

# create CA certificate
openssl req -config "$(dirname $0)"/ssl.cnf -new -sha256 -nodes -extensions v3_ca -out ./data/certs/ca.csr -keyout ./data/certs/ca.key
openssl req -config "$(dirname $0)"/ssl.cnf -key ./data/certs/ca.key -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out ./data/certs/ca.crt

# Create certificate for DB
openssl req -config "$(dirname $0)"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./data/certs/pg.key -out ./data/certs/pg.csr -extensions server_client_cert
openssl x509 -req -in ./data/certs/pg.csr -days 1200 -CA ./data/certs/ca.crt -CAkey ./data/certs/ca.key -set_serial 01 -out ./data/certs/pg.crt -extensions server_client_cert -extfile "$(dirname $0)"/ssl.cnf
