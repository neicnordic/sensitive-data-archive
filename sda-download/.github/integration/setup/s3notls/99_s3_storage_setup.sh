#!/bin/bash
set -e

sudo apt install -y s3cmd

cd dev_utils || exit 1

# Make buckets if they don't exist already 
s3cmd -c s3cmd-notls.conf mb s3://archive || true

# Upload test file
s3cmd -c s3cmd-notls.conf put archive_data/4293c9a7-dc50-46db-b79a-27ddc0dad1c6 s3://archive/4293c9a7-dc50-46db-b79a-27ddc0dad1c6
