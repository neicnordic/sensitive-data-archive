#!/bin/bash

pip3 install s3cmd

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
export C4GH_PASSPHRASE

ENC_MD5=$(md5sum largefile.c4gh | cut -d' ' -f 1)
ENC_SHA=$(sha256sum largefile.c4gh | cut -d' ' -f 1)

## check that all containers are running
docker ps -a

## case1 cancel message arrives before verification has started
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/case1/file.c4gh

docker pause verify

CORRID="$(uuidgen)"
## publish message to trigger ingestion
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

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user case1 \
        --arg filepath case1/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body"

RETRY_TIMES=0
until docker logs ingest --since="$now" 2>&1 | grep "File marked as archived"; do
    echo "case1 waiting for ingestion to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for ingest to complete, logs:"
        docker logs --since="$now" ingest
        exit 1
    fi
    sleep 10
done

cancel_payload=$(
    jq -r -c -n \
        --arg type cancel \
        --arg user case1 \
        --arg filepath case1/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

cancel_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$cancel_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$cancel_body"

status=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "SELECT status from local_ega.files where inbox_path='case1/file.c4gh' and elixir_id='case1';")

if [ "$status" != "DISABLED" ]; then
    echo "cancel before verification failed, expected DISABLED got: $status"
    docker logs ingest --since="$now"
    exit 1
fi

docker unpause verify

RETRY_TIMES=0
until docker logs verify --since="$now" 2>&1 | grep "is disabled, stopping verification"; do
    echo "case1 waiting for verify to abort"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for verify to abort, logs:"
        docker logs --since="$now" verify
        exit 1
    fi
    sleep 10
done

## case2 cancel message arrives before verification has completed
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/case2/file.c4gh

CORRID="$(uuidgen)"
## publish message to trigger ingestion
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

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user case2 \
        --arg filepath case2/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body"

RETRY_TIMES=0
until docker logs ingest --since="$now" 2>&1 | grep "File marked as archived"; do
    echo "case2 waiting for ingestion to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for ingest to complete, logs:"
        docker logs --since="$now" ingest
        exit 1
    fi
    sleep 10
done

cancel_payload=$(
    jq -r -c -n \
        --arg type cancel \
        --arg user case2 \
        --arg filepath case2/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

cancel_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$cancel_payload" \
        '$ARGS.named'
)

RETRY_TIMES=0
until docker logs verify --since="$now" 2>&1 | grep "Received work"; do
    echo "case2 waiting for verification to start"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for verify to start, logs:"
        docker logs --since="$now" ingest
        exit 1
    fi
    sleep 1
done

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$cancel_body"

RETRY_TIMES=0
until docker logs verify --since="$now" 2>&1 | grep "is disabled, stopping verification"; do
    echo "case2 waiting for verify to abort"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for verify to abort, logs:"
        docker logs --since="$now" verify
        exit 1
    fi
    sleep 10
done

## case3 cancel message arrives before stable ID has been set
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/case3/file.c4gh

CORRID="$(uuidgen)"
## publish message to trigger ingestion
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

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user case3 \
        --arg filepath case3/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body"

RETRY_TIMES=0
until docker logs verify --since="$now" 2>&1 | grep "File marked completed"; do
    echo "case3 waiting for verification to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for ingest to complete, logs:"
        docker logs --since="$now" verify
        exit 1
    fi
    sleep 10
done

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES((SELECT DISTINCT file_id from sda.file_event_log WHERE correlation_id = '$CORRID'), 'disabled', '$CORRID');"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "SELECT id, event, correlation_id from sda.file_event_log ORDER BY id;"

decrypted_checksums=$(
    curl -s -k -u test:test \
        -H "content-type:application/json" \
        -X POST https://localhost:15672/api/queues/test/verified/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r '.[0].payload|fromjson|.decrypted_checksums|tostring'
)

finalize_payload=$(
    jq -r -c -n \
        --arg type accession \
        --arg user case3 \
        --arg filepath case3/file.c4gh \
        --arg accession_id "EGAF74900000001" \
        --argjson decrypted_checksums "$decrypted_checksums" \
        '$ARGS.named|@base64'
)

finalize_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "accessionIDs" \
        --arg payload_encoding base64 \
        --arg payload "$finalize_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$finalize_body"

RETRY_TIMES=0
until docker logs finalize --since="$now" 2>&1 | grep "is disabled, stopping work"; do
    echo "case3 waiting for finalize to abort"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 6 ]; then
        echo "::error::Time out while waiting for finalize to abort, logs:"
        docker logs --since="$now" finalize
        exit 1
    fi
    sleep 10
done

## case4 restart ingestion of a canceled file
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/case4/file.c4gh

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "SELECT sda.register_file('case4/file.c4gh' 'case4');"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "UPATE local_ega.files set status = 'DISABLED' where inbox_path='case4/file.c4gh' and elixir_id='case4';"

CORRID="$(uuidgen)"
## publish message to trigger ingestion
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

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user case4 \
        --arg filepath case4/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body"

RETRY_TIMES=0
until docker logs ingest --since="$now" 2>&1 | grep "File marked as archived"; do
    echo "case4 waiting for ingestion to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 60 ]; then
        echo "::error::Time out while waiting for ingest to complete, logs:"
        docker logs --since="$now" ingest
        exit 1
    fi
    sleep 10
done

## cases with multiple ingestion workers
## case5 cancel message arrives before ingestion has completed
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

s3cmd -c s3cmd.conf put largefile.c4gh s3://inbox/case5/file.c4gh

CORRID="$(uuidgen)"
## publish message to trigger ingestion
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

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user case5 \
        --arg filepath case5/file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "files" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -s -k -u test:test "https://localhost:15672/api/exchanges/test/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
    -e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
    neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
    -t -A -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id) VALUES((SELECT DISTINCT file_id from sda.file_event_log WHERE correlation_id = '$CORRID'), 'disabled', '$CORRID');"

RETRY_TIMES=0
until docker logs ingest --since="$now" 2>&1 | grep "is disabled, stopping ingestion"; do
    echo "case5 waiting for ingestion to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 12 ]; then
        echo "::error::Time out while waiting for ingest to complete, logs:"
        docker logs --since="$now" ingest
        exit 1
    fi
    sleep 10
done
