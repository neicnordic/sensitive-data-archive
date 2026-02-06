#!/bin/bash
set -e

cd dev_utils || exit 1

# Make buckets if they don't exist already 
s3cmd -c s3cmd-notls.conf mb s3://archive-1 || true
s3cmd -c s3cmd-notls.conf mb s3://archive-2 || true

# Make buckets if they don't exist already
s3cmd -c s3cmd-notls.conf --host localhost:9001 --host-bucket localhost:9001 mb s3://archive-1 || true
s3cmd -c s3cmd-notls.conf --host localhost:9001 --host-bucket localhost:9001 mb s3://archive-2 || true

# Upload test file
s3cmd -c s3cmd-notls.conf put archive_data/test_file s3://archive-1/00000000-0000-0000-0000-000000000001
s3cmd -c s3cmd-notls.conf put archive_data/test_file s3://archive-2/00000000-0000-0000-0000-000000000002

# Upload test file
s3cmd -c s3cmd-notls.conf --host localhost:9001 --host-bucket localhost:9001 put archive_data/test_file s3://archive-1/00000000-0000-0000-0000-000000000003
s3cmd -c s3cmd-notls.conf --host localhost:9001 --host-bucket localhost:9001 put archive_data/test_file s3://archive-2/00000000-0000-0000-0000-000000000004

