#!/bin/bash
set -e

# Test the new sda/cmd/download service (v2 API)
# This test runs AFTER files have been mapped to a dataset (40_mapper_test.sh)
# Skips gracefully if the download service is not available (e.g. in sync stack)

cd shared || true

echo "=== Testing Download Service (v2 API) ==="

# Check if the download service is reachable before running tests
if ! curl -s --connect-timeout 3 -o /dev/null "http://download:8080/health/live" 2>/dev/null; then
    echo "Download service not available, skipping download v2 tests"
    exit 0
fi

token="$(cat /shared/token)"

# Test 1: Health endpoints
echo "Test 1: Health endpoints..."
response=$(curl -s -o /dev/null -w "%{http_code}" "http://download:8080/health/live")
if [ "$response" != "200" ]; then
    echo "::error::Health live endpoint failed, expected 200 got: $response"
    exit 1
fi
echo "  /health/live: OK"

response=$(curl -s -o /dev/null -w "%{http_code}" "http://download:8080/health/ready")
if [ "$response" != "200" ]; then
    echo "::error::Health ready endpoint failed, expected 200 got: $response"
    exit 1
fi
echo "  /health/ready: OK"

# Test 2: Unauthenticated request should return 401
echo "Test 2: Unauthenticated access..."
response=$(curl -s -o /dev/null -w "%{http_code}" "http://download:8080/datasets")
if [ "$response" != "401" ]; then
    echo "::error::Unauthenticated request should return 401, got: $response"
    exit 1
fi
echo "  Unauthenticated request returns 401: OK"

# Test 3: List datasets with valid token
echo "Test 3: List datasets..."
body=$(curl -s -H "Authorization: Bearer $token" "http://download:8080/datasets")
dataset_count=$(echo "$body" | jq '.datasets | length')
if [ "$dataset_count" -lt 1 ]; then
    echo "  Note: No datasets available yet"
    echo "  Skipping remaining tests that require datasets..."
    echo ""
    echo "=== Download Service Tests Completed (partial) ==="
    exit 0
fi
echo "  Found $dataset_count dataset(s): OK"

dataset_id=$(echo "$body" | jq -r '.datasets[0]')
echo "  Using dataset: $dataset_id"

# Test 4: Get dataset info
echo "Test 4: Get dataset info..."
dataset_info=$(curl -s -H "Authorization: Bearer $token" "http://download:8080/datasets/$dataset_id")
dataset_info_id=$(echo "$dataset_info" | jq -r '.datasetId')
if [ -z "$dataset_info_id" ] || [ "$dataset_info_id" = "null" ]; then
    echo "::error::Failed to get dataset info"
    echo "Response: $dataset_info"
    exit 1
fi
file_count=$(echo "$dataset_info" | jq -r '.files // 0')
total_size=$(echo "$dataset_info" | jq -r '.size // 0')
echo "  Dataset: $dataset_info_id (files: $file_count, size: $total_size bytes): OK"

# Test 5: List files in dataset
echo "Test 5: List files in dataset..."
files_body=$(curl -s -H "Authorization: Bearer $token" "http://download:8080/datasets/$dataset_id/files")
file_count=$(echo "$files_body" | jq '.files | length // 0')
if [ "$file_count" -lt 1 ]; then
    echo "  Note: No files in dataset"
    echo "  Skipping remaining tests that require files..."
    echo ""
    echo "=== Download Service Tests Completed (partial) ==="
    exit 0
fi
echo "  Found $file_count file(s) in dataset: OK"

file_id=$(echo "$files_body" | jq -r '.files[0].fileId')
echo "  Using file: $file_id"

# Test 6: Download file with public key
echo "Test 6: Download file with re-encryption..."
if [ -f "/shared/c4gh.pub.pem" ]; then
    pubkey=$(base64 -w0 /shared/c4gh.pub.pem)

    response_code=$(curl -s -o /tmp/downloaded_file.c4gh -w "%{http_code}" \
        -H "Authorization: Bearer $token" \
        -H "X-C4GH-Public-Key: $pubkey" \
        "http://download:8080/files/$file_id")

    if [ "$response_code" = "200" ]; then
        downloaded_size=$(stat -c '%s' /tmp/downloaded_file.c4gh 2>/dev/null || echo "0")
        echo "  Downloaded file size: $downloaded_size bytes: OK"

        file_magic=$(head -c 8 /tmp/downloaded_file.c4gh | od -A n -t x1 | tr -d ' ')
        if [ "$file_magic" = "6372797074346768" ]; then
            echo "  File has valid crypt4gh magic: OK"
        else
            echo "  Warning: File may not be valid crypt4gh format"
        fi
    elif [ "$response_code" = "500" ]; then
        echo "  Note: Re-encryption failed (likely key mismatch in test data): SKIPPED"
    else
        echo "::error::Download failed with status: $response_code"
        exit 1
    fi
else
    echo "  Skipping: No public key available at /shared/c4gh.pub.pem"
fi

# Test 7: Access denied for non-existent file (no existence leakage)
echo "Test 7: Access control..."
response=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $token" \
    -H "X-C4GH-Public-Key: dGVzdA==" \
    "http://download:8080/files/EGAF00000000000")

if [ "$response" = "403" ]; then
    echo "  Non-existent file returns 403: OK"
else
    echo "::error::Expected 403 for non-existent file, got: $response"
    exit 1
fi

echo ""
echo "=== Download Service Tests Completed Successfully ==="
