#!/bin/sh

if [ "$(id -u)" -eq 0 ]; then
    apt-get -qq update && apt-get -qq install -y jq xxd
fi

cd dev_utils || exit 1

local_uid=$(stat -c '%u' .)

token="$(bash keys/sign_jwt.sh ES256 /keys/jwt.key)"
sed -i "s/^access_token=.*/access_token=$token/" proxyS3

mkdir -p /local_tmp/certs
cp /certs/* /local_tmp/certs/
cp /keys/*pub /local_tmp/certs/

chown "$local_uid":"$local_uid" /local_tmp/certs/*
chmod 600 /local_tmp/certs/*.key
