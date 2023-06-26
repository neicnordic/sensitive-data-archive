#!/bin/bash

cd dev_utils || exit 1

# ------------------
# Test Health Endpoint

check_health=$(curl -o /dev/null -s -w "%{http_code}\n" http://localhost:8080/health)

if [ "$check_health" != "200" ]; then
    echo "Health endpoint does not respond properly"
    echo "got: ${check_health}"
    exit 1
fi

echo "Health endpoint is ok"

# ------------------
# Test empty token

check_401=$(curl -o /dev/null -s -w "%{http_code}\n" http://localhost:8080/metadata/datasets)

if [ "$check_401" != "401" ]; then
    echo "no token provided should give 401"
    echo "got: ${check_401}"
    exit 1
fi

echo "got correct response when no token provided"

check_405=$(curl -X POST -o /dev/null -s -w "%{http_code}\n" http://localhost:8080/metadata/datasets )

if [ "$check_405" != "405" ]; then
    echo "POST should not be allowed"
    echo "got: ${check_405}"
    exit 1
fi

echo "got correct response when POST method used"

# ------------------
# Test good token

token=$(curl "http://localhost:8000/tokens" | jq -r  '.[0]')

## Test datasets endpoint

check_dataset=$(curl -H "Authorization: Bearer $token" http://localhost:8080/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found"

## Test datasets/files endpoint

check_files=$(curl -H "Authorization: Bearer $token" "http://localhost:8080/metadata/datasets/https://doi.example/ty009.sfrrss/600.45asasga/files" | jq -r '.[0].fileId')

if [ "$check_files" != "urn:neic:001-002" ]; then
    echo "file with id urn:neic:001-002 not found"
    echo "got: ${check_files}"
    exit 1
fi

echo "expected file found"

# Test file can be decrypted
## test also the files endpoint

C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
export C4GH_PASSPHRASE

crypt4gh decrypt --sk c4gh.sec.pem < dummy_data.c4gh > old-file.txt

curl -H "Authorization: Bearer $token" "http://localhost:8080/files/urn:neic:001-002" --output test-download.txt


cmp --silent old-file.txt test-download.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
fi

# ------------------
# Test get visas failed

token=$(curl "http://localhost:8000/tokens" | jq -r  '.[1]')

## Test datasets endpoint

check_empty_token=$(curl -o /dev/null -s -w "%{http_code}\n" -H "Authorization: Bearer $token" http://localhost:8080/metadata/datasets)

if [ "$check_empty_token" != "200" ]; then
    echo "response for empty token is not 200"
    echo "got: ${check_empty_token}"
    exit 1
fi

echo "got correct response when token has no permissions"

# ------------------
# Test token with untrusted sources
# for this test we don't attach a list of trusted sources

token=$(curl "http://localhost:8000/tokens" | jq -r  '.[2]')

## Test datasets endpoint

check_dataset=$(curl -H "Authorization: Bearer $token" http://localhost:8080/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found for token from untrusted source"
