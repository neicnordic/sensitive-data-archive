#!/bin/sh
set -e

cd shared || true

ENC_SHA=$(sha256sum NA12878_20k_b37.bam.c4gh | cut -d' ' -f 1)
ENC_MD5=$(md5sum NA12878_20k_b37.bam.c4gh | cut -d' ' -f 1)

## get correlation id from message
CORRID=$(
curl -s -X POST \
    -H "content-type:application/json" \
    -u guest:guest http://rabbitmq:15672/api/queues/sda/verified/get \
    -d '{"count":2,"encoding":"auto","ackmode":"ack_requeue_true"}' | jq -r .[1].properties.correlation_id
)

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

encrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$ENC_SHA" \
        --arg md5 "$ENC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

cancel_payload=$(
    jq -r -c -n \
        --arg type cancel \
        --arg user test@dummy.org \
        --arg filepath NA12878_20k_b37.bam.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

cancel_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$cancel_payload" \
        '$ARGS.named'
)

curl -k -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$cancel_body" | jq

# check database to verify file status
if [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.file_event_log where file_id = '$CORRID' order by id DESC LIMIT 1")" != "disabled" ]; then
    echo "canceling file failed"
    exit 1
fi


# check database to verify file archive location and path has been unset
if [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT 1 FROM sda.files WHERE id = '$CORRID' AND archive_file_path = '' AND archive_location IS NULL")" != "1" ]; then
    echo "canceling file failed"
    exit 1
fi

cat >/shared/direct <<EOD
[default]
access_key=access
secret_key=secretKey
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3:9000
host_bucket = s3:9000
human_readable_sizes = false
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

# Verify that archived file is removed
result=$(s3cmd -c direct ls s3://archive1/test_dummy.org/"$CORRID")
if [ "$result" != "" ]; then
    echo "file with id $CORRID was not removed from archive"
    exit 1
fi
result=$(s3cmd -c direct ls s3://archive2/test_dummy.org/"$CORRID")
if [ "$result" != "" ]; then
    echo "file with id $CORRID was not removed from archive"
    exit 1
fi
result=$(s3cmd -c direct ls s3://backup1/test_dummy.org/"$CORRID")
if [ "$result" != "" ]; then
    echo "file with id $CORRID was not removed from backup"
    exit 1
fi

# re-ingest cancelled file
ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath NA12878_20k_b37.bam.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -k -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body" | jq

RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/verified/ | jq -r '.messages_ready')" -eq 5 ]; do
    echo "waiting for verify to complete after re-ingestion"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to complete after re-ingestion"
        exit 1
    fi
    sleep 2
done

echo "re-ingestion after verify test completed successfully"
