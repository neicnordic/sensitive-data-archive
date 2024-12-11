#!/bin/bash

if [ "$STORAGETYPE" = s3notls ]; then
    exit 0
fi

cd dev_utils || exit 1

# Get a token, set up variables
token=$(curl -s --cacert certs/ca.pem "https://localhost:8000/tokens" | jq -r  '.[0]')

if [ -z "$token" ]; then
    echo "Failed to obtain token"
    exit 1
fi

dataset="https://doi.example/ty009.sfrrss/600.45asasga"
file="dummy_data"
clientkey=$(base64 -w0 client.pub.pem)
bad_token=token2=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJyZXF1ZXN0ZXJAZGVtby5vcmciLCJhdWQiOlsiYXVkMiIsImF1ZDMiXSwiYXpwIjoiYXpwIiwic2NvcGUiOiJvcGVuaWQiLCJpc3MiOiJodHRwczovL2RlbW8uZXhhbXBsZSIsImV4cCI6OTk5OTk5OTk5OSwiaWF0IjoxNTYxNjIxOTEzLCJqdGkiOiI2YWQ3YWE0Mi0zZTljLTQ4MzMtYmQxNi03NjVjYjgwYzIxMDIifQ.ncUyjNytxqS9bqLnsbjv6D839PnHVw-anQS4bFpAs20

# Test error codes and error messages returned to the user

# try to download encrypted file without sending a public key
resp=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3/$dataset/$file")
if ! echo "$resp" | grep -q "c4gh public key is missing from the header"; then
    echo "Incorrect response, expected 'c4gh public key is missing from the header' got $resp"
    exit 1
fi

resp=$(curl --cacert certs/ca.pem -H "Authorization: Bearer $token" "https://localhost:8443/s3/$dataset/$file" -s -o /dev/null -w "%{http_code}")
if [ "$resp" -ne 400 ]; then
    echo "Incorrect response with missing public key, expected 400 got $resp"
    exit 1
fi

# try to download encrypted file with a bad public key
resp=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: YmFkIGtleQ==" "https://localhost:8443/s3/$dataset/$file")
if ! echo "$resp" | grep -q "file re-encryption error"; then
    echo "Incorrect response, expected 'file re-encryption error' got $resp"
    exit 1
fi

resp=$(curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: YmFkIGtleQ==" "https://localhost:8443/s3/$dataset/$file" -s -o /dev/null -w "%{http_code}")
if [ "$resp" -ne 500 ]; then
    echo "Incorrect response with missing public key, expected 500 got $resp"
fi

# try to download encrypted file from instance that serves unencrypted files

resp=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:9443/s3/$dataset/$file")
if ! echo "$resp" | grep -q "downloading encrypted data is not supported"; then
    echo "Incorrect response, expected 'downloading encrypted data is not supported' got $resp"
    exit 1
fi

resp=$(curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:9443/s3/$dataset/$file" -s -o /dev/null -w "%{http_code}")
if [ "$resp" -ne 400 ]; then
    echo "Incorrect response, expected 400 got $resp"
    exit 1
fi


# try to download a file the user doesn't have access to

resp=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $bad_token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3/$dataset/$file")
if ! echo "$resp" | grep -q "get visas failed"; then
    echo "Incorrect response, expected 'get visas failed' got $resp"
    exit 1
fi

resp=$(curl --cacert certs/ca.pem -H "Authorization: Bearer $bad_token" -H "Client-Public-Key: $clientkey" "https://localhost:8443/s3/$dataset/$file" -s -o /dev/null -w "%{http_code}")
if [ "$resp" -ne 401 ]; then
    echo "Incorrect response, expected 401 got $resp"
    exit 1
fi

# try to download a file that does not exist

resp=$(curl -s --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:9443/s3/$dataset/nonexistentfile")
if [ -n "$resp" ]; then
    echo "Incorrect response, expected no error message, got $resp"
    exit 1
fi

resp=$(curl --cacert certs/ca.pem -H "Authorization: Bearer $token" -H "Client-Public-Key: $clientkey" "https://localhost:9443/s3/$dataset/nonexistentfile" -s -o /dev/null -w "%{http_code}")
if [ "$resp" -ne 404 ]; then
    echo "Incorrect response, expected 404 got $resp"
    exit 1
fi

echo "OK"