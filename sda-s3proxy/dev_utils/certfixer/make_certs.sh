#!/bin/sh

set -e

out_dir="/cert_gen"

# install openssl if it's missing
if [ ! "$(command -v openssl)" ]; then
    apk add openssl
fi

script_dir="$(dirname "$0")"
mkdir -p "$out_dir"

# list all certificates we want, so that we can check if they already exist
s3_certs="/s3_certs/CAs/public.crt /s3_certs/public.crt /s3_certs/private.key"
mq_certs="/mq_certs/ca.crt /mq_certs/mq.crt /mq_certs/mq.key"
pub_cert="/pubcert/public.crt"
proxy_certs="/proxy_certs/ca.crt /proxy_certs/client.crt /proxy_certs/client.key /proxy_certs/proxy.crt /proxy_certs/proxy.key"
keys="/keys/jwt.key /keys/sda-sda-svc-auth.pub"
targets="$s3_certs $mq_certs $pub_cert $proxy_certs $keys"

echo ""
echo "Checking certificates"
recreate="false"
# check if certificates exist
for target in $targets; do
    if [ ! -f "$target" ]; then
        recreate="true"
        break
    fi
done

# only recreate certificates if any certificate is missing
if [ "$recreate" = "false" ]; then
    echo "certificates already exists"
    exit 0
fi

# create CA certificate
openssl req -config "$script_dir/ssl.cnf" -new -sha256 -nodes -extensions v3_ca -out "$out_dir/ca.csr" -keyout "$out_dir/ca-key.pem"
openssl req -config "$script_dir/ssl.cnf" -key "$out_dir/ca-key.pem" -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out "$out_dir/ca.crt"

# Create certificate for MQ
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/mq.key" -out "$out_dir/mq.csr" -extensions server_cert
openssl x509 -req -in "$out_dir/mq.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/mq.crt" -extensions server_cert -extfile "$script_dir/ssl.cnf"

# Create certificate for Proxy
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/proxy.key" -out "$out_dir/proxy.csr" -extensions server_cert
openssl x509 -req -in "$out_dir/proxy.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/proxy.crt" -extensions server_cert -extfile "$script_dir/ssl.cnf"

# Create certificate for minio
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/s3.key" -out "$out_dir/s3.csr" -extensions server_cert
openssl x509 -req -in "$out_dir/s3.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/s3.crt" -extensions server_cert -extfile "$script_dir/ssl.cnf"

# Create client certificate
openssl req -config "$script_dir/ssl.cnf" -new -nodes -newkey rsa:4096 -keyout "$out_dir/client.key" -out "$out_dir/client.csr" -extensions client_cert -subj "/CN=admin"
openssl x509 -req -in "$out_dir/client.csr" -days 1200 -CA "$out_dir/ca.crt" -CAkey "$out_dir/ca-key.pem" -set_serial 01 -out "$out_dir/client.crt" -extensions client_cert -extfile "$script_dir/ssl.cnf"

# create EC256 key for signing the JWT tokens
openssl ecparam -genkey -name prime256v1 -noout -out $out_dir/jwt.key
openssl ec -in $out_dir/jwt.key -outform PEM -pubout >$out_dir/jwt.pub

# fix permissions
chmod 644 "$out_dir"/*
chown -R root:root "$out_dir"/*
chmod 600 "$out_dir"/*-key.pem

# move certificates to volumes
mkdir -p /s3_certs/CAs
cp -p "$out_dir/ca.crt" /s3_certs/CAs/public.crt
cp -p "$out_dir/s3.crt" /s3_certs/public.crt
cp -p "$out_dir/s3.key" /s3_certs/private.key

cp -p "$out_dir/ca.crt" /mq_certs/ca.crt
cp -p "$out_dir/mq.crt" /mq_certs/mq.crt
cp -p "$out_dir/mq.key" /mq_certs/mq.key

cp -p "$out_dir/ca.crt" /pubcert/public.crt

cp -p "$out_dir/ca.crt" /proxy_certs/ca.crt
cp -p "$out_dir/client.crt" /proxy_certs/client.crt
cp -p "$out_dir/client.key" /proxy_certs/client.key
cp -p "$out_dir/proxy.crt" /proxy_certs/proxy.crt
cp -p "$out_dir/proxy.key" /proxy_certs/proxy.key
cp -p "$out_dir/jwt.pub" /keys/sda-sda-svc-auth.pub
cp -p "$out_dir/jwt.key" /keys/jwt.key
