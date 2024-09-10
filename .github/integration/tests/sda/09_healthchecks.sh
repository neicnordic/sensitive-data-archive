#!/bin/sh
set -e

# install tools if missing
for t in curl  jq ; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done


# Test the s3inbox's healthchecks, GET /health and HEAD /
response="$(curl -s -k -LI "http://s3inbox:8000" -o /dev/null -w "%{http_code}\n")"
if [ "$response" != "200" ]; then
	echo "Bad health response from HEAD /, expected 200 got: $response"
	exit 1
fi

response="$(curl -s -k -LI "http://s3inbox:8000/health" -o /dev/null -w "%{http_code}\n")"
if [ "$response" != "200" ]; then
	echo "Bad health response from /health, expected 200 got: $response"
	exit 1
fi

echo "Healthcheck tests completed successfully"