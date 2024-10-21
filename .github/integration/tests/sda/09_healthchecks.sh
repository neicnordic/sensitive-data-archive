#!/bin/sh
set -e

# Test the s3inbox's healthchecks, GET /health and HEAD /
response="$(curl -s -k -LI "http://s3inbox:8000" -o /dev/null -w "%{http_code}\n")"
if [ "$response" != "200" ]; then
	echo "Bad health response from HEAD /, expected 200 got: $response"
	exit 1
fi

response="$(curl -s -k -L "http://s3inbox:8000/health" -o /dev/null -w "%{http_code}\n")"
if [ "$response" != "200" ]; then
	echo "Bad health response from /health, expected 200 got: $response"
	exit 1
fi

echo "Healthcheck tests completed successfully"