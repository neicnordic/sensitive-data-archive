#!/bin/bash
set -e

cd shared || true

if [ "$STORAGETYPE" = "posix" ]; then
    exit 0
fi

# check bucket for synced files
for file in NA12878.bai NA12878_20k_b37.bai; do
    RETRY_TIMES=0
    until [ "$(s3cmd -c direct ls s3://sync/test_dummy.org/"$file")" != "" ]; do
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq 30 ]; then
            echo "::error::Time out while waiting for files to be synced"
            exit 1
        fi
        sleep 2
    done
done

echo "files synced successfully"