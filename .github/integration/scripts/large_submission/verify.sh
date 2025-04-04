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

submission_size=60000

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
    inbox_path="inbox/test_dummy.org/test-file-$i.c4gh"
    fileID=$(psql -U postgres -h postgres -d sda -At -c "SELECT sda.register_file('$inbox_path', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi
    psql -U postgres -h postgres -d sda -At -c "UPDATE sda.files SET header = '565f129f4727ecb986814e0278dd2d0b21521159ac19a1292403826302d62462' WHERE id = '$fileID';" >/dev/null
    psql -U postgres -h postgres -d sda -At -c "SELECT sda.set_archived('$fileID', '$fileID', '$inbox_path', '$i','$(echo "0$i" | sha256sum)', 'SHA256');" >/dev/null

    ENC_SHA=$(echo $i | sha256sum | cut -d' ' -f 1)
    ENC_MD5=$(echo $i | md5sum | cut -d' ' -f 1)

    properties=$(
        jq -c -n \
            --argjson delivery_mode 2 \
            --arg correlation_id "$fileID" \
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

    verify_payload=$(
        jq -r -c -n \
            --arg archive_path "$fileID" \
            --arg file_id "$fileID" \
            --arg user test@dummy.org \
            --arg filepath "$inbox_path" \
            --argjson encrypted_checksums "$encrypted_checksums" \
            --argjson re_verify false \
            '$ARGS.named|@base64'
    )

    verify_body=$(
        jq -c -n \
            --arg vhost sda \
            --arg name sda \
            --argjson properties "$properties" \
            --arg routing_key "archived" \
            --arg payload_encoding base64 \
            --arg payload "$verify_payload" \
            '$ARGS.named'
    )

    curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
        -H 'Content-Type: application/json;charset=UTF-8' \
        -d "$verify_body" >/dev/null

    i=$((i + 1))
done

sleep 10
RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/archived | jq '.messages')" -eq 0 ]; do
    echo "waiting for messages to be processed"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to complete the work"
        exit 1
    fi
    sleep 10
done

RETRY_TIMES=0
until [ $((stream_size+submission_size)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; do
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "Messages not moved to error"
        exit 1
    fi
    sleep 2
done

echo "test for verifying $submission_size files completed successfully"
