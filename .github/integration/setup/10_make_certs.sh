#!/bin/sh

cd .github/integration || exit 1

mkdir -p certs

# create CA certificate
openssl req -config setup/ssl.cnf -new -sha256 -nodes -extensions v3_ca -out ./certs/ca.csr -keyout ./certs/ca-key.pem
openssl req -config setup/ssl.cnf -key ./certs/ca-key.pem -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out ./certs/ca.pem

# Create certificate for MQ
openssl req -config setup/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/mq-key.pem -out ./certs/mq.csr -extensions server_cert
openssl x509 -req -in ./certs/mq.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/mq.pem -extensions server_cert -extfile setup/ssl.cnf

# Create certificate for DB
openssl req -config setup/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/db-key.pem -out ./certs/db.csr -extensions server_cert
openssl x509 -req -in ./certs/db.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/db.pem -extensions server_cert -extfile setup/ssl.cnf

# Create client certificate
openssl req -config setup/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/client-key.pem -out ./certs/client.csr -extensions client_cert -subj "/CN=lega_in/CN=admin/"
openssl x509 -req -in ./certs/client.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/client.pem -extensions client_cert -extfile setup/ssl.cnf

chmod 644 certs/*
