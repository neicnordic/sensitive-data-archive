#!/bin/sh
set -e

token="$(curl http://oidc:8080/tokens | jq -r '.[0]')"
result="$(curl -sk -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq '. | length')"
if [ "$result" -ne 2 ]; then
    echo "wong number of files returned for user test@dummy.org"
    echo "expected 4 got $result"
    exit 1
fi