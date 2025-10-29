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

check_401=$(curl -o /dev/null -s -w "%{http_code}\n" -H "SDA-Client-Version: v0.3.0" http://localhost:8080/metadata/datasets)

if [ "$check_401" != "401" ]; then
    echo "no token provided should give 401"
    echo "got: ${check_401}"
    exit 1
fi

echo "got correct response when no token provided"

check_405=$(curl -X POST -o /dev/null -s -w "%{http_code}\n" -H "SDA-Client-Version: v0.3.0" http://localhost:8080/metadata/datasets)

if [ "$check_405" != "405" ]; then
    echo "POST should not be allowed"
    echo "got: ${check_405}"
    exit 1
fi

echo "got correct response when POST method used"

# ------------------
# Test good token

token=$(curl -s "http://localhost:8000/tokens" | jq -r  '.[0]')

## Test datasets endpoint

check_dataset=$(curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" http://localhost:8080/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found"

## Test datasets/files endpoint

check_files=$(curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "http://localhost:8080/metadata/datasets/https://doi.example/ty009.sfrrss/600.45asasga/files" | jq -r '.[0].fileId')

if [ "$check_files" != "urn:neic:001-002" ]; then
    echo "file with id urn:neic:001-002 not found"
    echo "got: ${check_files}"
    exit 1
fi

echo "expected file found"

# Test file can be decrypted
## test also the files endpoint

C4GH_PASSPHRASE=$(yq .c4gh.passphrase config.yaml)
export C4GH_PASSPHRASE

crypt4gh decrypt -s c4gh.sec.pem -f dummy_data.c4gh && mv dummy_data old-file.txt

# first try downloading from download instance serving encrypted data, should fail
curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "http://localhost:8080/files/urn:neic:001-002" --output test-download.txt

if ! grep -q "downloading unencrypted data is not supported" "test-download.txt"; then
    echo "wrong response when trying to download unencrypted data from encrypted endpoint"
    exit 1
fi

# now try downloading from download instance serving unencrypted data
curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "http://localhost:9080/files/urn:neic:001-002" --output test-download.txt


cmp --silent old-file.txt test-download.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
fi

# downloading from download instance serving unencrypted data
curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "http://localhost:9080/files/urn:neic:001-002?startCoordinate=0&endCoordinate=2" --output test-part.txt

dd if=old-file.txt ibs=1 skip=0 count=2 > old-part.txt

cmp --silent old-part.txt test-part.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

# downloading from download instance serving unencrypted data
curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "http://localhost:9080/files/urn:neic:001-002?startCoordinate=7&endCoordinate=14" --output test-part2.txt

dd if=old-file.txt ibs=1 skip=7 count=7 > old-part2.txt

cmp --silent old-part2.txt test-part2.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

# ------------------
# Test get visas failed

token=$(curl -s "http://localhost:8000/tokens" | jq -r  '.[1]')

## Test datasets endpoint

check_empty_token=$(curl -o /dev/null -s -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" http://localhost:8080/metadata/datasets)

if [ "$check_empty_token" != "200" ]; then
    echo "response for empty token is not 200"
    echo "got: ${check_empty_token}"
    exit 1
fi

echo "got correct response when token has no permissions"

# ------------------
# Test token with untrusted sources
# for this test we don't attach a list of trusted sources

token=$(curl -s "http://localhost:8000/tokens" | jq -r  '.[2]')

## Test datasets endpoint

check_dataset=$(curl -s -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" http://localhost:8080/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found for token from untrusted source"

echo "OK"