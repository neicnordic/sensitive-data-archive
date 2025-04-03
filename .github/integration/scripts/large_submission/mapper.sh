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

: >"/shared/accessions.txt"

i=1
while [ $i -le $((submission_size)) ]; do
    user="test@dummy.org"
    inbox_path="inbox/test-file-$i.c4gh"
    fileID=$(psql -U postgres -h postgres -d sda -At -c "SELECT sda.register_file('$inbox_path', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    # This is only so that the file event logs gets a non null correlation ID.
    psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, message) VALUES('$fileID', 'submitted', '$fileID', '$user', '{}');" >/dev/null

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
mapping_payload=$(
    jq -Rrcn \
        --arg user test@dummy.org \
        --arg dataset_id EGAD74900000101 \
        --slurpfile accession_ids /shared/accessions.txt \
        '$ARGS.named'
)
echo "$mapping_payload" >/shared/payload

token="$(cat /shared/token)"
curl -s -d @/tmp/dataset \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $token" \
    -X POST http://localhost:8090/dataset/create >/dev/null

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
