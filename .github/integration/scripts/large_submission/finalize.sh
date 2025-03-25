#!/bin/sh
set -e

for t in curl jq postgresql-client uuid-runtime; do
    if [ ! "$(command -v $t)" ]; then
        if [ "$(id -u)" != 0 ]; then
            echo "$t is missing, unable to install it"
            exit 1
        fi

        apt-get -o DPkg::Lock::Timeout=60 update >/dev/null
        apt-get -o DPkg::Lock::Timeout=60 install -y "$t" >/dev/null
    fi
done

submission_size=100000

## empty all queues ##
for q in accession archived backup completed inbox ingest mappings verified; do
    curl -s -k -u guest:guest -X DELETE "http://rabbitmq:15672/api/queues/sda/$q/contents"
done
## truncate database
psql -U postgres -h postgres -d sda -At -c "TRUNCATE TABLE sda.files CASCADE;"

stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

i=1
while [ $i -le $((submission_size)) ]; do
    user="test@dummy.org"
    inbox_path="inbox/test-file-$i.c4gh"
    fileID=$(psql -U postgres -h postgres -d sda -At -c "SELECT sda.register_file('$inbox_path', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi
    resp=$(psql -U postgres -h postgres -d sda -At -c "UPDATE sda.files SET archive_file_path = '$fileID', archive_file_size = '$i' WHERE id = '$fileID';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "failed to update file $resp"
        exit 1
    fi
    psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, message) VALUES('$fileID', 'verified', '$fileID', 'test-user', '{\"uploaded\": \"message\"}');" >/dev/null

    properties=$(
        jq -c -n \
            --argjson delivery_mode 2 \
            --arg correlation_id "$fileID" \
            --arg content_encoding UTF-8 \
            --arg content_type application/json \
            '$ARGS.named'
    )

    DEC_SHA=$(echo $i | sha256sum | cut -d' ' -f 1)
    decrypted_checksums=$(
        jq -c -n \
            --arg sha256 "$DEC_SHA" \
            '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
    )

    accession_id="urn:uuid:$(uuidgen)"
    accession_payload=$(
        jq -r -c -n \
            --arg type accession \
            --arg user "$user" \
            --arg filepath "$inbox_path" \
            --arg accession_id "$accession_id" \
            --argjson decrypted_checksums "$decrypted_checksums" \
            '$ARGS.named|@base64'
    )

    accession_body=$(
        jq -c -n \
            --arg vhost sda \
            --arg name sda \
            --argjson properties "$properties" \
            --arg routing_key "accession" \
            --arg payload_encoding base64 \
            --arg payload "$accession_payload" \
            '$ARGS.named'
    )

    curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
        -H 'Content-Type: application/json;charset=UTF-8' \
        -d "$accession_body" >/dev/null

    i=$((i + 1))
done

RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/accession | jq '.messages')" -eq 0 ]; do
    echo "waiting for messages to be processed"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for finalize to compete the work"
        echo "This is currently expected"
        exit 1
    fi
    sleep 10
done

RETRY_TIMES=0
until [ $((stream_size+submission_size)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; do
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "Messages not moved to error"
        echo "This is currently expected"
        exit 1
    fi
    sleep 2
done

echo "test for finalizing $submission_size files completed successfully"
