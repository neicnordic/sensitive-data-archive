#!/bin/bash

if [ "$STORAGETYPE" != "s3" ]; then
    exit 0
fi

cd shared || true

# test that listing all buckets is not allowed
list_all_buckets=$(s3cmd -c s3cfg -q ls 2>&1)
if ! [[ "$list_all_buckets" =~ "Forbidden" ]]; then
    echo "list buckets should fail"
    exit 1
fi

# test disallowed characters
disallowed_characters=$(s3cmd -c s3cfg -q put s3cfg s3://test_dummy.org/fai\| 2>&1)
if ! [[ "$disallowed_characters" =~ "filepath contains disallowed characters" ]];then
    echo "test with disallowed characters failed"
    exit 1
fi

# test error message when using invalid token
cp s3cfg bads3cfg
sed -i "s/access_token=.*/access_token=invalid/" bads3cfg

unathorized=$(s3cmd -c bads3cfg -q ls s3://test_dummy.org/ 2>&1)
if ! [[ "$unathorized" =~ "Unauthorized" ]]; then
    echo "testing unathorized call failed"
    exit 1
fi

aws configure set aws_access_key_id dummy # Value dont matter as we authenticate with the aws_session_token
aws configure set aws_secret_access_key dummy # Value dont matter as we authenticate with the aws_session_token
aws configure set aws_session_token "$(grep 'access_token' s3cfg | cut -d "=" -f 2 | xargs)"

listResponse=$(aws s3api list-objects --endpoint http://s3inbox:8000 --bucket test_dummy.org)
if (( $(echo "$listResponse" | grep -c "test_dummy.org") == 0 )); then
  echo "found no files for users when listing towards s3inbox with ListObjects call"
  exit 1
fi

listV2Response=$(aws s3api list-objects --endpoint http://s3inbox:8000 --bucket test_dummy.org)
if (( $(echo "$listV2Response" | grep -c "test_dummy.org") == 0 )); then
  echo "found no files for users when listing towards s3inbox with ListObjectsV2 call"
  exit 1
fi


aws configure set aws_session_token "$(cat token)"

listResponse=$(aws s3api list-objects --endpoint http://s3inbox:8000 --bucket testu_lifescience-ri.eu)
if ! (( $(echo "$listResponse" | grep -c "test_dummy.org") == 0 )); then
  echo "found other users files when listing towards s3inbox with ListObjects call"
  exit 1
fi

listV2Response=$(aws s3api list-objects-v2 --endpoint http://s3inbox:8000 --bucket testu_lifescience-ri.eu)
if ! (( $(echo "$listV2Response" | grep -c "test_dummy.org") == 0 )); then
  echo "found other users files when listing towards s3inbox with ListObjectsV2 call"
  exit 1
fi


echo "s3cmd error messages tested successfully"
