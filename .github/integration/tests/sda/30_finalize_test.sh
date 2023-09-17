#!/bin/bash
set -e

cd shared || true

i=1
while [ $i -le "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/verified/ | jq -r '.messages_ready')" ]; do
    ## get correlation id from upload message
    MSG=$(
        curl -s -X POST \
            -H "content-type:application/json" \
            -u guest:guest http://rabbitmq:15672/api/queues/sda/verified/get \
            -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}'
    )

    corrid=$(jq -r '.[0].properties.correlation_id' <<< "$MSG")
    user=$(jq -r '.[0].payload|fromjson|.user' <<< "$MSG")
    filepath=$(jq -r '.[0].payload|fromjson|.filepath' <<< "$MSG")
    decrypted_checksums=$(jq -r '.[0].payload|fromjson|.decrypted_checksums|tostring' <<< "$MSG")

    ## publish message to trigger backup
    properties=$(
        jq -c -n \
            --argjson delivery_mode 2 \
            --arg correlation_id "$corrid" \
            --arg content_encoding UTF-8 \
            --arg content_type application/json \
            '$ARGS.named'
    )

    accession_payload=$(
        jq -r -c -n \
            --arg type accession \
            --arg user "$user" \
            --arg filepath "$filepath" \
            --arg accession_id "EGAF7490000000$i" \
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
        -d "$accession_body"

    i=$(( i + 1 ))
done

echo "waiting for finalize to complete"

until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/completed/ | jq -r '.messages_ready')" -eq 2 ]; do
    echo "waiting for finalize to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for finalize to complete"
        exit 1
    fi
    sleep 2
done

echo "finalize completed successfully"
