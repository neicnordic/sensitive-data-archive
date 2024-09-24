#!/bin/bash
set -e

stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

bad_payload=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding string \
        --arg payload "{I give you bad JSON!}" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$bad_payload" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "Bad payload not moved to error"
    exit 1
fi

payload=$(
    jq -r -c -n \
        --arg type wrong \
        --arg user dummy \
        '$ARGS.named|@base64'
)

bad_type=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$bad_type" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "Bad type not moved to error"
    exit 1
fi

echo "bad messages test completed successfully"
