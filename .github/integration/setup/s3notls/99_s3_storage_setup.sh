#!/bin/bash

pip3 install s3cmd

cd dev_utils || exit 1

# Make buckets if they don't exist already
s3cmd -c s3cmd-notls.conf mb s3://inbox || true
s3cmd -c s3cmd-notls.conf mb s3://archive || true

# Upload test file
for file in dummy_data.c4gh largefile.c4gh empty.c4gh truncated1.c4gh truncated2.c4gh wrongly_encrypted.c4gh test_db_file.c4gh; do
    s3cmd -c s3cmd-notls.conf put $file s3://inbox/$file
done
