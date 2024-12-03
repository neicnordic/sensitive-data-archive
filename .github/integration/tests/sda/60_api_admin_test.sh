#!/bin/sh
set -e

token="$(curl http://oidc:8080/tokens | jq -r '.[0]')"
result="$(curl -sk -L "http://api:8080/users/test@dummy.org/files" -H "Authorization: Bearer $token" | jq '. | length')"
if [ "$result" -ne 2 ]; then
    echo "wrong number of files returned for user test@dummy.org"
    echo "expected 4 got $result"
    exit 1
fi

## trigger re-verification of EGAF74900000001
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/file/verify/EGAF74900000001")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification, expected 200 got: $resp"
	exit 1
fi

## failure on wrong accession ID
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/file/verify/EGAF74900054321")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification, expected 404 got: $resp"
	exit 1
fi

## trigger re-verification of dataset SYNC-001-12345
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/dataset/verify/SYNC-001-12345")"
if [ "$resp" != "200" ]; then
	echo "Error when starting re-verification of dataset, expected 200 got: $resp"
	exit 1
fi

## expect failure of missing dataset
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -X PUT "http://api:8080/dataset/verify/SYNC-999-12345")"
if [ "$resp" != "404" ]; then
	echo "Error when starting re-verification of missing dataset, expected 404 got: $resp"
	exit 1
fi