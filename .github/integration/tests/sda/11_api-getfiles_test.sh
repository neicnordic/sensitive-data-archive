#!/bin/sh
set -e

# Test the API files endpoint
token="$(curl http://oidc:8080/tokens | jq -r '.[0]')"
curl -k -L "http://api:8080/files" -H "Authorization: Bearer $token"
response="$(curl -k -L "http://api:8080/files" -H "Authorization: Bearer $token" | jq -r 'sort_by(.inboxPath)|.[-1].fileStatus')"
if [ "$response" != "uploaded" ]; then
	echo "API returned incorrect value, expected ready got: $response"
	exit 1
fi

echo "api test completed successfully"
