#!/bin/bash

cd dev_utils || exit 1

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

# ------------------
# Test Health Endpoint

check_health=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem https://localhost:8443/health)

if [ "$check_health" != "200" ]; then
    echo "Health endpoint does not respond properly"
    echo "got: ${check_health}"
    exit 1
fi

echo "Health endpoint is ok"

check_health_header=$(curl -o /dev/null  -s -w "%{http_code}\n" -LI --cacert certs/ca.pem https://localhost:8443/)

if [ "$check_health_header" != "200" ]; then
    echo "Head request to health endpoint does not respond properly"
    echo "got: ${check_health_header}"
    exit 1
fi

echo "Head method health endpoint is ok"

# ------------------
# Test empty token

check_401=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem -H "SDA-Client-Version: v0.3.0" https://localhost:8443/metadata/datasets)

if [ "$check_401" != "401" ]; then
    echo "no token provided should give 401"
    echo "got: ${check_401}"
    exit 1
fi

echo "got correct response when no token provided"

check_405=$(curl -o /dev/null -s -w "%{http_code}\n" -X POST --cacert certs/ca.pem -H "SDA-Client-Version: v0.3.0" https://localhost:8443/metadata/datasets)

if [ "$check_405" != "405" ]; then
    echo "POST should not be allowed"
    echo "got: ${check_405}"
    exit 1
fi

echo "got correct response when POST method used"

# ------------------
# Test good token

token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')

# Test Client Version Header
# We assume the app is configured to require a minimum version (v0.2.0).

# Fail - missing header (expected 412 Precondition Failed)
check_missing_header=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/metadata/datasets")

if [ "$check_missing_header" != "412" ]; then
    echo "Client Version Test FAIL: missing header should return 412"
    echo "got: ${check_missing_header}"
    exit 1
fi
echo "Client Version Test OK: Missing header correctly returns 412"

# Fail - insufficient version (e.g., v0.1.0, Expected 412)
check_insufficient_version=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.1.0" "https://localhost:8443/metadata/datasets")

if [ "$check_insufficient_version" != "412" ]; then
    echo "Client Version Test FAIL: insufficient version (v0.1.0) should return 412"
    echo "got: ${check_insufficient_version}"
    exit 1
fi
echo "Client Version Test OK: Insufficient version (v0.1.0) correctly returns 412"

# Success - sufficient version (e.g., v0.2.0, Expected 200)
check_sufficient_version=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.2.0" "https://localhost:8443/metadata/datasets")

if [ "$check_sufficient_version" != "200" ]; then
    echo "Client Version Test FAIL: sufficient version (v0.2.0) should pass version check and return 200"
    echo "got: ${check_sufficient_version}"
    exit 1
fi
echo "Client Version Test OK: sufficient version (v0.2.0) correctly returns 200"

# Success - newer version (e.g., v1.0.0, Expected 200)
check_newer_version=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v1.0.0" "https://localhost:8443/metadata/datasets")

if [ "$check_newer_version" != "200" ]; then
    echo "Client Version Test FAIL: Newer version (v1.0.0) should pass version check and return 200"
    echo "got: ${check_newer_version}"
    exit 1
fi
echo "Client Version Test OK: Newer version (v1.0.0) correctly returns 200"

## Test datasets endpoint

check_dataset=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" https://localhost:8443/metadata/datasets | jq -r '.[0]')

if [ "$check_dataset" != "https://doi.example/ty009.sfrrss/600.45asasga" ]; then
    echo "dataset https://doi.example/ty009.sfrrss/600.45asasga not found"
    echo "got: ${check_dataset}"
    exit 1
fi

echo "expected dataset found"

## Test datasets/files endpoint

check_files=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:8443/metadata/datasets/https://doi.example/ty009.sfrrss/600.45asasga/files" | jq -r '.[0].fileId')

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

curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/files/urn:neic:001-002" --output test-download.txt

cmp --silent old-file.txt test-download.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/files/urn:neic:001-002?startCoordinate=0&endCoordinate=2" --output test-part.txt

dd if=old-file.txt ibs=1 skip=0 count=2 > old-part.txt

cmp --silent old-part.txt test-part.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/files/urn:neic:001-002?startCoordinate=7&endCoordinate=14" --output test-part2.txt

dd if=old-file.txt ibs=1 skip=7 count=7 > old-part2.txt

cmp --silent old-part2.txt test-part2.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:9443/files/urn:neic:001-002?startCoordinate=70000&endCoordinate=140000" --output test-part3.txt

dd if=old-file.txt ibs=1 skip=70000 count=70000 > old-part3.txt

cmp --silent old-part3.txt test-part3.txt
status=$?
if [[ $status = 0 ]]; then
    echo "Files are the same"
else
    echo "Files are different"
    exit 1
fi

# test that downloads of decrypted files from a download instance that
# serves only encrypted files (here running at port 8443) should fail
curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" "https://localhost:8443/files/urn:neic:001-002" --output test-download-fail.txt

if ! grep -q "downloading unencrypted data is not supported" test-download-fail.txt; then
    echo "got unexpected response when trying to download unencrypted data from encrypted endpoint"
exit 1
fi

# ------------------
# Test get visas failed

token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[1]')

## Test datasets endpoint

check_empty_token=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET -I --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" https://localhost:8443/metadata/datasets)

if [ "$check_empty_token" != "200" ]; then
    echo "response for empty token is not 200"
    echo "got: ${check_empty_token}"
    exit 1
fi

echo "got correct response when token has no permissions"

# ------------------
# Test token with untrusted sources
# for this test we attach a list of trusted sources

token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[2]')

## Test datasets endpoint

check_empty_token=$(curl -o /dev/null -s -w "%{http_code}\n" -X GET -I --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "SDA-Client-Version: v0.3.0" https://localhost:8443/metadata/datasets)

if [ "$check_empty_token" != "200" ]; then
    echo "response for token with untrusted sources is not 200"
    echo "got: ${check_empty_token}"
    exit 1
fi

echo "got correct response when token permissions from untrusted sources"

# cleanup
rm old-file.txt old-part*.txt test-download*.txt test-part*.txt

echo "OK"