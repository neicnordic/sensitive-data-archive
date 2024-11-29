#!/bin/bash
set -e

cd shared || true

if [ -z "$SYNCTEST" ]; then
    echo "sync not tested"
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

echo "waiting for sync-api to send messages"
RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/catch_all.dead/ | jq -r '.messages_ready')" -eq 5 ]; do
    echo "waiting for sync-api to send messages"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for sync-api to send messages"
        exit 1
    fi
    sleep 2
done

echo "sync test completed successfully"
