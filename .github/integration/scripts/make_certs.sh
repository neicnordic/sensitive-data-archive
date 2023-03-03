#!/bin/sh

# install openssl if it's missing
if [ ! "$(command -v openssl)" ]; then
    if [ "$(id -u)" != 0 ]; then
        echo "openssl is missing, unable to install it"
        exit 1
    fi

    apk add --no-cache openssl
fi

out_dir="/cert_gen"
script_dir="$(dirname "$0")"
mkdir -p "$out_dir"

# list all certificates we want, so that we can check if they already exist
if [ -f "$out_dir/ca.crt" ]; then
    echo "certificates already exists"
    cp -r "$out_dir/." /temp/certs
    exit 0
fi

# create CA certificate
openssl req -config "$script_dir/ssl.cnf" -new -sha256 -nodes -extensions v3_ca -out "$out_dir/ca.csr" -keyout "$out_dir/ca-key.pem"
openssl req -config "$script_dir/ssl.cnf" -key "$out_dir/ca-key.pem" -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out "$out_dir/ca.crt"

# Create certificate for MQ
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/mq.key" -out "$out_dir/mq.csr" -extensions server_cert
openssl x509 -req -in "$out_dir/mq.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/mq.crt" -extensions server_cert -extfile "$script_dir/ssl.cnf"

# Create client certificate
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/client.key" -out "$out_dir/client.csr" -extensions client_cert -subj "/CN=admin"
openssl x509 -req -in "$out_dir/client.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/client.crt" -extensions client_cert -extfile "$script_dir/ssl.cnf"

# fix permissions
chmod 644 "$out_dir"/*

# move certificates to volumes

cp -p "$out_dir/ca.crt" /certs/ca.crt
cp -p "$out_dir/mq.crt" /certs/mq.crt
cp -p "$out_dir/mq.key" /certs/mq.key
chown 100:101 /certs/mq.*

chmod 600 /certs/*.key

cp -p "$out_dir/ca.crt" /client_certs/ca.crt
cp -p "$out_dir/client.crt" /client_certs/
cp -p "$out_dir/client.key" /client_certs/

# needed if testing locally
mkdir -p /temp/certs
cp -r "$out_dir/." /temp/certs

