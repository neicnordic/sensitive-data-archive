#!/bin/sh
set -e

MQHOST="localhost:15672"
if [ -f /.dockerenv ]; then
	MQHOST="mq:15671"
fi

## publish message to trigger dac or unknow type message
properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

dac_payload=$(
    jq -r -c -n \
        --arg type dac \
        --arg title "DAC only used for testing all functionalities" \
        --arg description "DAC only used for testing all functionalities" \
        --arg accession_id "EGAD74900000101" \
        '$ARGS.named|@base64'
)

dac_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "dac" \
        --arg payload_encoding base64 \
        --arg payload "$dac_payload" \
        '$ARGS.named'
)

curl -s --cacert /tmp/certs/ca.crt -u guest:guest "https://$MQHOST/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$dac_body" | jq


echo "waiting for message to be in catch_all.dead"
RETRY_TIMES=0
until [ "$(curl -s --cacert /tmp/certs/ca.crt -u guest:guest https://$MQHOST/api/queues/sda/catch_all.dead/ | jq -r '.messages_ready')" -eq 1 ]; do
    echo "waiting for dac message"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dac messages"
        exit 1
    fi
    sleep 2
done

echo "unknown message (type dac) test completed successfully"