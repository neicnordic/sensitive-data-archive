#!/bin/bash

sed -i 's/BACKUP_TYPE=s3/BACKUP_TYPE=sftp/g' dev_utils/env.backup
