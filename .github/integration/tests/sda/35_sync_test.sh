#!/bin/bash
set -e

cd shared || true

# check bucket for synced files
for file in NA12878.bam.c4gh NA12878_20k_b37.bam.c4gh; do
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