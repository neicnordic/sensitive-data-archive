#!/bin/sh
set -e

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y curl jq openssl postgresql-client >/dev/null

for n in download finalize inbox ingest mapper sync verify; do
    echo "creating credentials for: $n"
    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"

    ## password and permissions for MQ
    body_data=$(jq -n -c --arg password "$n" --arg tags none '$ARGS.named')
    curl -s -u guest:guest -X PUT "http://rabbitmq:15672/api/users/$n" -H "content-type:application/json" -d "${body_data}"
    curl -s -u guest:guest -X PUT "http://rabbitmq:15672/api/permissions/sda/$n" -H "content-type:application/json" -d '{"configure":"","write":"sda","read":".*"}'

done

# create EC256 key for signing the JWT tokens
mkdir -p /shared/keys/pub
if [ ! -f "/shared/keys/jwt.key" ]; then
    openssl ecparam -genkey -name prime256v1 -noout -out /shared/keys/jwt.key
    openssl ec -in /shared/keys/jwt.key -outform PEM -pubout >/shared/keys/pub/jwt.pub
    chmod 644 /shared/keys/pub/jwt.pub /shared/keys/jwt.key
fi

token="$(bash /scripts/sign_jwt.sh ES256 /shared/keys/jwt.key)"

cat >/shared/s3cfg <<EOD
[default]
access_key=test_dummy.org
secret_key=test_dummy.org
access_token=$token
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3inbox:8000
host_bucket = s3inbox:8000
human_readable_sizes = true
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

## create crypt4gh key
if [ ! -f "/shared/c4gh.sec.pem" ]; then
    curl -s -L https://github.com/neicnordic/crypt4gh/releases/download/v1.7.4/crypt4gh_linux_x86_64.tar.gz | tar -xz -C /shared/ && chmod +x /shared/crypt4gh
    /shared/crypt4gh generate -n /shared/c4gh -p c4ghpass
fi