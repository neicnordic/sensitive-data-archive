#!/bin/sh

mkdir -p certs

# create CA certificate
openssl req -config "$(dirname "$0")"/ssl.cnf -new -sha256 -nodes -extensions v3_ca -out ./certs/ca.csr -keyout ./certs/ca-key.pem
openssl req -config "$(dirname "$0")"/ssl.cnf -key ./certs/ca-key.pem -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out ./certs/ca.pem

# Create certificate for MQ
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/mq-key.pem -out ./certs/mq.csr -extensions server_cert
openssl x509 -req -in ./certs/mq.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/mq.pem -extensions server_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for DB
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/db-key.pem -out ./certs/db.csr -extensions server_cert
openssl x509 -req -in ./certs/db.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/db.pem -extensions server_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for minio
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/s3-key.pem -out ./certs/s3.csr -extensions server_cert
openssl x509 -req -in ./certs/s3.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/s3.pem -extensions server_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create client certificate
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout ./certs/client-key.pem -out ./certs/client.csr -extensions client_cert
openssl x509 -req -in ./certs/client.csr -days 1200 -CA ./certs/ca.pem -CAkey ./certs/ca-key.pem -set_serial 01 -out ./certs/client.pem -extensions client_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create rsa key-pair for sftp client authentication
ssh-keygen -t rsa -f ./certs/sftp-key -P "test"
mv ./certs/sftp-key ./certs/sftp-key.pem

chmod 644 ./certs/*
