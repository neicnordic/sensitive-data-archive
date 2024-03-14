#!/bin/bash

sed -i 's/ARCHIVE_TYPE=.*/ARCHIVE_TYPE=s3seekable/g' dev_utils/env.download

