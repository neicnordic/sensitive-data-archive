#!/bin/bash
set -ex

if [ "$STORAGETYPE" != "s3" ]; then
    exit 0
fi

cd shared || true

# test that listing all buckets is not allowed
set +e
list_all_buckets=$(s3cmd -c s3cfg -q ls 2>&1)
set -e
if ! [[ "$list_all_buckets" =~ "not allowed response" ]]; then
    echo "list buckets should fail"
    exit 1
fi

# test disallowed characters
set +e
disallowed_characters=$(s3cmd -c s3cfg -q put s3cfg s3://test_dummy.org/fai\| 2>&1)
set -e
if ! [[ "$disallowed_characters" =~ "filepath contains disallowed characters" ]];then
    echo "test with disallowed characters failed"
    exit 1
fi

# test error message when using invalid token
cp s3cfg bads3cfg
sed -i "s/access_token=.*/access_token=invalid/" bads3cfg

set +e
unathorized=$(s3cmd -c bads3cfg -q ls s3://test_dummy.org/ 2>&1)
set -e
if ! [[ "$unathorized" =~ "not authorized" ]]; then
    echo "testing unathorized call failed"
    exit 1
fi


echo "s3cmd error messages tested successfully"
