#!/bin/sh
set -e

# install requirements if it's missing
for r in openssl openjdk8-jre-base; do
    if [ ! "$(command -v "$r")" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$r is missing, unable to install it"
            exit 1
        fi

        apk add --no-cache "$r"
    fi
done

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

# Create certificate for the servers
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/server.key" -out "$out_dir/mq.csr" -extensions server_cert
openssl x509 -req -in "$out_dir/mq.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/server.crt" -extensions server_cert -extfile "$script_dir/ssl.cnf"

# Create client certificate
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/client.key" -out "$out_dir/client.csr" -extensions client_cert -subj "/CN=admin"
openssl x509 -req -in "$out_dir/client.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/client.crt" -extensions client_cert -extfile "$script_dir/ssl.cnf"

if [ -n "$KEYSTORE_PASSWORD" ]; then
    # Create Java keystore
    mkdir -p /certs/java
    if [ -f /certs/java/cacerts ]; then
        rm /certs/java/cacerts
    fi
    keytool -import -trustcacerts -file "$out_dir/ca.crt" -alias CegaCA -storetype JKS -keystore /certs/java/cacerts -storepass "$KEYSTORE_PASSWORD" -noprompt
    openssl pkcs12 -export -out /certs/java/keystore.p12 -inkey "$out_dir/server.key" -in "$out_dir/server.crt" -passout pass:"$KEYSTORE_PASSWORD"
fi
# fix permissions
chmod 644 "$out_dir"/*

# move certificates to volumes
cp -p "$out_dir/ca.crt" /certs/ca.crt

cp -p "$out_dir/server.crt" /certs/server.crt
cp -p "$out_dir/server.key" /certs/server.key
chown 1000:1000 /certs/server.*

cp -p "$out_dir/server.crt" /certs/mq.crt
cp -p "$out_dir/server.key" /certs/mq.key
chown 100:101 /certs/mq.*

cp -p "$out_dir/server.crt" /certs/db.crt
cp -p "$out_dir/server.key" /certs/db.key
chown 70:70 /certs/db.*

chmod 600 /certs/*.key

cp -p "$out_dir/ca.crt" /client_certs/ca.crt
cp -p "$out_dir/client.crt" /client_certs/
cp -p "$out_dir/client.key" /client_certs/
chmod 600 /client_certs/*.key

# needed if testing locally
mkdir -p /temp/certs
cp -r "$out_dir/." /temp/certs

ls -la /certs/