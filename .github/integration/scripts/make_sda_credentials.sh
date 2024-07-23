#!/bin/sh
set -e

URI=http://rabbitmq:15672

# Postgres requires a client certificate, so this is a simple way of detecting if TLS is enabled or not.
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi

apt-get -o DPkg::Lock::Timeout=60 update > /dev/null
apt-get -o DPkg::Lock::Timeout=60 install -y curl jq openssh-client openssl postgresql-client >/dev/null

pip install --upgrade pip > /dev/null
pip install aiohttp Authlib joserfc requests > /dev/null

for n in download finalize inbox ingest mapper sync verify; do
    echo "creating credentials for: $n"
    psql -U postgres -h postgres -d sda -c "ALTER ROLE $n LOGIN PASSWORD '$n';"
    psql -U postgres -h postgres -d sda -c "GRANT base TO $n;"

    ## password and permissions for MQ
    body_data=$(jq -n -c --arg password "$n" --arg tags none '$ARGS.named')
    curl -s -u guest:guest -X PUT -k "$URI/api/users/$n" -H "content-type:application/json" -d "${body_data}"
    curl -s -u guest:guest -X PUT -k "$URI/api/permissions/sda/$n" -H "content-type:application/json" -d '{"configure":"","write":"sda","read":".*"}'
done

# create EC256 key for signing the JWT tokens
mkdir -p /shared/keys/pub
if [ ! -f "/shared/keys/jwt.key" ]; then
    echo "creating jwt key"
    openssl ecparam -genkey -name prime256v1 -noout -out /shared/keys/jwt.key
    openssl ec -in /shared/keys/jwt.key -outform PEM -pubout >/shared/keys/pub/jwt.pub
    chmod 644 /shared/keys/pub/jwt.pub /shared/keys/jwt.key
fi

echo "creating token"
token="$(python /scripts/sign_jwt.py)"

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
if [ ! -f "/shared/crypt4gh" ]; then
    echo "downloading crypt4gh"
    latest_c4gh=$(curl --retry 100 -sL https://api.github.com/repos/neicnordic/crypt4gh/releases/latest | jq -r '.name')
    curl --retry 100 -s -L "https://github.com/neicnordic/crypt4gh/releases/download/$latest_c4gh/crypt4gh_linux_x86_64.tar.gz" | tar -xz -C /shared/ && chmod +x /shared/crypt4gh
fi
if [ ! -f "/shared/c4gh.sec.pem" ]; then
    echo "creating crypth4gh key"
    /shared/crypt4gh generate -n /shared/c4gh -p c4ghpass
fi
if [ ! -f "/shared/sync.sec.pem" ]; then
    echo "creating sync crypth4gh key"
    /shared/crypt4gh generate -n /shared/sync -p syncPass
fi

if [ ! -f "/shared/keys/ssh" ]; then
    ssh-keygen -o -a 256 -t ed25519 -f /shared/keys/ssh -N ""
    pubKey="$(cat /shared/keys/ssh.pub)"
    cat >/shared/users.json <<EOD
[
    {
        "username": "dummy@example.com",
        "uid": 1,
        "passwordHash": "\$2b\$12\$1gyKIjBc9/cT0MYkXX24xe1LjEUjNwgL4rEk8fDoO.vDQZzWkqrn.",
        "gecos": "dummy user",
        "sshPublicKey": ["$pubKey"],
        "enabled": null
    }
]
EOD
fi

## download grpcurl
if [ ! -f "/shared/grpcurl" ]; then
    echo "downloading grpcurl"
    latest_grpculr=$(curl --retry 100 -sL https://api.github.com/repos/fullstorydev/grpcurl/releases/latest | jq -r '.name' | sed -e 's/v//')
    curl --retry 100 -s -L "https://github.com/fullstorydev/grpcurl/releases/download/v${latest_grpculr}/grpcurl_${latest_grpculr}_linux_x86_64.tar.gz" | tar -xz -C /shared/ && chmod +x /shared/grpcurl
fi