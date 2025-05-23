#!/bin/bash
set -e

cd shared || true

## map files to dataset
properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

mappings=$(
    jq -c -n \
        '$ARGS.positional' \
        --args "EGAF74900000001" \
        --args "EGAF74900000002"
)

mapping_payload=$(
    jq -r -c -n \
        --arg type mapping \
        --arg dataset_id EGAD74900000101 \
        --argjson accession_ids "$mappings" \
        '$ARGS.named|@base64'
)

mapping_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "mappings" \
        --arg payload_encoding base64 \
        --arg payload "$mapping_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$mapping_body" | jq

# check DB for dataset contents
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select count(id) from sda.file_dataset where dataset_id = (select id from sda.datasets where stable_id = 'EGAD74900000101');")" -eq 2 ]; do
    echo "waiting for mapper to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be mapped"
        exit 1
    fi
    sleep 2
done

## check that files has been removed form the inbox
for file in NA12878.bam.c4gh NA12878_20k_b37.bam.c4gh; do
    result=$(s3cmd -c direct ls s3://inbox/test_dummy.org/"$file")
    if [ "$result" != "" ]; then
        echo "Failed to remove $file from inbox"
        exit 1
    fi
done

until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.file_event_log where file_id = (select id from sda.files where stable_id = 'EGAF74900000002') order by started_at DESC LIMIT 1;")" = "ready" ]; do
    echo "waiting for files be ready"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for files to be ready"
        exit 1
    fi
    sleep 2
done

until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.dataset_event_log where dataset_id = 'EGAD74900000101' order by event_date DESC LIMIT 1;")" = "registered" ]; do
    echo "waiting for dataset be registered"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be registered"
        exit 1
    fi
    sleep 2
done

echo "dataset mapped successfully"

## release dataset
release_payload=$(
    jq -r -c -n \
        --arg type release \
        --arg dataset_id EGAD74900000101 \
        '$ARGS.named'
)

release_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "mappings" \
        --arg payload "$release_payload" \
        --arg payload_encoding string \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$release_body" | jq 

until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.dataset_event_log where dataset_id = 'EGAD74900000101' order by event_date DESC LIMIT 1;")" = "released" ]; do
    echo "waiting for dataset be released"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be released"
        exit 1
    fi
    sleep 2
done

echo "dataset released successfully"

## deprecate dataset
deprecate_payload=$(
    jq -r -c -n \
        --arg type deprecate \
        --arg dataset_id EGAD74900000101 \
        '$ARGS.named'
)

deprecate_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "mappings" \
        --arg payload "$deprecate_payload" \
        --arg payload_encoding string \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$deprecate_body" | jq

until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.dataset_event_log where dataset_id = 'EGAD74900000101' order by event_date DESC LIMIT 1")" = "deprecated" ]; do
    echo "waiting for dataset be deprecated"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be deprecated"
        exit 1
    fi
    sleep 2
done

echo "dataset deprecated successfully"

mappings=$(
    jq -c -n \
        '$ARGS.positional' \
        --args "SYNC-123-00003" \
        --args "SYNC-123-00004"
)

mapping_payload=$(
    jq -r -c -n \
        --arg type mapping \
        --arg dataset_id SYNC-001-12345 \
        --argjson accession_ids "$mappings" \
        '$ARGS.named|@base64'
)

mapping_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "mappings" \
        --arg payload_encoding base64 \
        --arg payload "$mapping_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$mapping_body" | jq

# check DB for dataset contents
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select count(id) from sda.file_dataset where dataset_id = (select id from sda.datasets where stable_id = 'SYNC-001-12345')")" -eq 2 ]; do
    echo "waiting for mapper to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for dataset to be mapped"
        exit 1
    fi
    sleep 2
done


## Use API to list the datasets
token="$(cat /shared/token)"
resp="$(curl -s -k -L -H "Authorization: Bearer $token" -X GET "http://api:8080/datasets/list" | jq '. | length')"
if [ "$resp" -ne 2 ]; then
	echo "Error when listing key hash, expected 2 entries got: $resp"
	exit 1
fi


echo "mapping test completed successfully"
