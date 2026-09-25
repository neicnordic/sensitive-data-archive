#!/bin/bash
set -e

cd shared || true

# clean ingest queue to simplify what is to come.
curl -s -u guest:guest -XDELETE http://rabbitmq:15672/api/queues/sda/inbox/contents

# encrypt file with wrong public key and upload to inbox
yes | /shared/crypt4gh encrypt -p sync.pub.pem -f NA12878.bam
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/bad.file.c4gh

# truncate file to simulate storage issues
dd if=NA12878_20k_b37.bam.c4gh bs=128 count=1024 of=truncated.c4gh
s3cmd -c s3cfg put truncated.c4gh s3://test_dummy.org/

# cut a file a few bytes into its second data segment (header 124 bytes + one
# 65564-byte segment + 5). A stream that ends 1-11 bytes into a segment makes
# the crypt4gh reader panic; verify must recover and move the file to error
# instead of crashing and re-processing the same message forever.
head -c 65693 NA12878_20k_b37.bam.c4gh > shortsegment.c4gh
s3cmd -c s3cfg put shortsegment.c4gh s3://test_dummy.org/

## test starts here

CORRID=$(
    curl -s -X POST \
        -H "content-type:application/json" \
        -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
)

stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

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
        --arg sha256 "$(echo "aa" | sha256sum | cut -d ' ' -f1)" \
        --arg md5 "$(echo "aa" | md5sum | cut -d ' ' -f1)" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

bad_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath bad.file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

bad_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$bad_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$bad_file" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "Bad file not moved to error"
    exit 1
fi

missing_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath missing.file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

FILEID=$(psql -U postgres -h postgres -d sda -At -c "SELECT DISTINCT(file_id) FROM sda.file_event_log WHERE file_id = '$CORRID';")
psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, user_id, message) VALUES('$FILEID', 'uploaded', 'test@dummy.org', '{\"uploaded\": \"message\"}');"

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

missing_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$missing_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$missing_file" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "missing file not moved to error"
    exit 1
fi

CORRID=$(
    curl -s -X POST \
        -H "content-type:application/json" \
        -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
)

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

truncated_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath truncated.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

truncated_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$truncated_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$truncated_file" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "truncated file not moved to error"
    exit 1
fi

# Drain the inbox message the upload produced (accounting: 3 uploads, 3 gets),
# but take the file id from the database by path so the correlation id is this
# file and not whatever sits at the front of the inbox queue.
curl -s -X POST \
    -H "content-type:application/json" \
    -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
    -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' > /dev/null

short_segment_id=$(psql -U postgres -h postgres -d sda -At -c "SELECT id FROM sda.files WHERE submission_file_path = 'shortsegment.c4gh' ORDER BY created_at DESC LIMIT 1;")
if [ -z "$short_segment_id" ]; then
    echo "::error::no file id found for shortsegment.c4gh; the inbox upload or ingest registration failed"
    exit 1
fi

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$short_segment_id" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

short_segment_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath shortsegment.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

short_segment=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$short_segment_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$short_segment" | jq

# verify must recover from the crypt4gh panic and set an error event for this
# specific file. If it instead crashed, the message would be redelivered forever
# and no verify error event would ever be written, so this poll times out and
# fails. Match user_id='verify' so an ingest error on another file cannot pass it.
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id = '$short_segment_id' AND user_id = 'verify' ORDER BY id DESC LIMIT 1;")" = "error" ]; do
    echo "waiting for verify to set an error event on the mid-segment file"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 20 ]; then
        echo "::error::Timed out waiting for verify to error the mid-segment file; verify may have crashed"
        exit 1
    fi
    sleep 2
done

echo "file errors test completed successfully"
