#!/bin/bash

if [ $UID -eq 0 ]; then
    apt-get -qq update && apt-get -qq install -y jq xxd
fi

# Function checking that a file was uploaded to the S3 backend
function check_output_status() {
    if [ "$1" -eq 0 ]; then
        echo -e "\u2705 Test passed, expected response found"
    else
        echo -e "\u274c Test failed, expected response not found"
        exit 1
    fi
}

cd dev_utils || exit 1

token="$(bash keys/sign_jwt.sh ES256 /keys/jwt.key)"
sed -i "s/^access_token=.*/access_token=$token/" proxyS3

# set correct host for S3 and proxy
sed -i "s/localhost:9000/s3:9000/g" directS3
sed -i "s/localhost:8000/s3_proxy:8000/g" proxyS3

s3cmd -c directS3 put README.md s3://test/some_user/ >/dev/null 2>&1 || exit 1

echo "- Testing allowed actions"

# Put file into bucket
echo "Trying to upload a file to user's bucket"
s3cmd -c proxyS3 put README.md s3://dummy/ >/dev/null 2>&1
check_output_status "$?"

# List objects
echo "Trying to list user's bucket"
s3cmd -c proxyS3 ls s3://dummy/ 2>&1 | grep -q "README.md"
check_output_status "$?"

# ---------- Test forbidden actions ----------
forbidden="Forbidden"
unauthorized="Unauthorized"
nobucket="NoSuchBucket"

echo "- Testing forbidden actions"

# Make bucket
echo "Trying to create bucket"
s3cmd -c proxyS3 mb s3://test_bucket 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Remove bucket
echo "Trying to remove bucket"
s3cmd -c proxyS3 rb s3://test 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# List buckets
echo "Trying to list all buckets"
s3cmd -c proxyS3 ls s3:// 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# List all objects in all buckets
echo "Trying to list all objects in all buckets"
s3cmd -c proxyS3 la s3:// 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Put file into another user's bucket
echo "Trying to upload a file to another user's bucket"
s3cmd -c proxyS3 put README.md s3://some_user/ 2>&1 | grep -q "$unauthorized"
check_output_status "$?"

# Get file from another user's bucket
echo "Trying to get a file from another user's bucket"
s3cmd -c proxyS3 get s3://some_user/README.md local_file.md 2>&1 | grep -q "$unauthorized"
check_output_status "$?"

# Get file from own bucket
echo "Trying to get a file from user's bucket"
echo "This is skipped due to being non functional"
# s3cmd -c proxyS3 get s3://dummy/README.md local_file.md 2>&1 | grep -q "does not exist"
# check_output_status "$?"

# Delete file from bucket
echo "Trying to delete a file from user's bucket"
s3cmd -c proxyS3 del s3://dummy/README.md 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Disk usage by buckets
echo "Trying to get disk usage for user's bucket"
s3cmd -c proxyS3 du s3://dummy 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Get various information about user's bucket
echo "Trying to get information about for user's bucket"
s3cmd -c proxyS3 info s3://dummy 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Get various information about user's file
echo "Trying to get information about user's file"
s3cmd -c proxyS3 info s3://dummy/README.md 2>&1 | grep -q "NoSuchKey"
check_output_status "$?"

# Move object
echo "Trying to move file to another location"
s3cmd -c proxyS3 mv s3://dummy/README.md s3://dummy/test 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Copy object
echo "Trying to copy file to another location"
s3cmd -c proxyS3 cp s3://dummy/README.md s3://dummy/test 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Modify access control list for file
echo "Trying to modify acl for user's file"
s3cmd -c proxyS3 setacl s3://dummy/README.md --acl-public 2>&1 | grep -q "$forbidden"
check_output_status "$?"

# Show multipart uploads - when multipart enabled, add all relevant tests
echo "Trying to list multipart uploads"
s3cmd -c proxyS3 multipart s3://dummy/ 2>&1 | grep -q "$nobucket"
check_output_status "$?"

# Enable/disable bucket access logging
echo "Trying to change the access logging for a bucket"
s3cmd -c proxyS3 accesslog s3://dummy/ 2>&1 | grep -q "$nobucket"
check_output_status "$?"

token="$(bash keys/sign_jwt.sh ES256 /keys/jwt.key yesterday)"
sed -i "s/^access_token=.*/access_token=$token/" proxyS3

# Test access with expired token
echo "Test access with expired token"
s3cmd -c proxyS3 ls s3://dummy/README.md 2>&1 | grep -q "$unauthorized"
check_output_status "$?"

echo "All tests have passed"
