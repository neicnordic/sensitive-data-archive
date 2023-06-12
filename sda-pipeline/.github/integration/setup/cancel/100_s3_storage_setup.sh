#!/bin/bash

pip3 install s3cmd

cd dev_utils || exit 1

# Make buckets if they don't exist already
s3cmd -c s3cmd.conf mb s3://inbox || true
s3cmd -c s3cmd.conf mb s3://archive || true

# Upload test file
s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/largefile.c4gh
