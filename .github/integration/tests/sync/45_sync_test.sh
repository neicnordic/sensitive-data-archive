#!/bin/bash
set -e

cd shared || true

if [ -z "$SYNCTEST" ]; then
    echo "sync not tested"
    exit 0
fi

# check bucket for synced files
RETRY_TIMES=0
until [ "$(s3cmd -c direct ls s3://sync-inbox/ | wc -l)" != 4 ]; do
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for files to be synced"
        exit 1
    fi
    sleep 2
done

echo "files synced successfully"

echo "waiting for sync-api to send messages"
RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/sync_files/ | jq -r '.messages_ready')" -eq 4 ]; do
    echo "waiting for sync-api to send messages"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for sync-api to send messages"
        exit 1
    fi
    sleep 2
done

echo "sync test completed successfully"
