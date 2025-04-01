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

rm /shared/accessions.txt /shared/payload /shared/message.json || true
touch "/shared/accessions.txt"

i=1
while [ $i -le $((submission_size)) ]; do
    user="test@dummy.org"
    inbox_path="inbox/test-file-$i.c4gh"
    fileID=$(psql -U postgres -h postgres -d sda -At -c "SELECT sda.register_file('$inbox_path', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    accession="urn:uuid:$(uuidgen)"
    resp=$(psql -U postgres -h postgres -d sda -At -c "UPDATE sda.files SET stable_id = '$accession' WHERE id = '$fileID';")
    if [ "$resp" != "UPDATE 1" ]; then
        echo "mark file ready failed $resp"
        exit 1
    fi
    echo "\"$accession\"" >>"/shared/accessions.txt"

    i=$((i + 1))
done

## map files to dataset
properties=$(
    jq -cn \
        --argjson delivery_mode 2 \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

mapping_payload=$(
    jq -Rrcn \
        --arg type mapping \
        --arg dataset_id EGAD74900000101 \
        --slurpfile accession_ids /shared/accessions.txt \
        '$ARGS.named|@base64'
)
echo "$mapping_payload" >/shared/payload

mapping_body=$(
    jq -Rcn \
        --arg vhost test \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "mappings" \
        --arg payload_encoding base64 \
        --rawfile payload "/shared/payload" \
        '$ARGS.named'
)
echo "$mapping_body" >/shared/message.json

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "@/shared/message.json" >/dev/null

RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/mappings | jq '.messages')" -eq 0 ]; do
    echo "waiting for dataset be registered"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for mapper to complete the work"
        exit 1
    fi
    sleep 10
done

echo "test for mapping a large dataset completed successfully"
