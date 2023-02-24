#!/bin/bash
set -e

if [ ! -f s3cmd.conf ]; then
  cat >>"s3cmd.conf" <<EOF
[default]
access_key=$(grep s3_access_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
secret_key=$(grep s3_secret_key sda-deploy-init/config/trace.yml | awk '{print $2}' | sed -e 's/\"//g')
check_ssl_certificate = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = https://localhost:9000
host_bucket = https://localhost:9000
use_https = True
socket_timeout = 30
ca_certs_file = sda-svc/files/ca.crt
EOF
fi

kubectl port-forward "$(kubectl get pods | grep minio | awk '{print $1}')" 9000:9000 &

# Wait for port forwarding to be active
sleep 3

s3cmd -c s3cmd.conf mb s3://inbox || true
s3cmd -c s3cmd.conf mb s3://archive || true
s3cmd -c s3cmd.conf mb s3://backup || true
