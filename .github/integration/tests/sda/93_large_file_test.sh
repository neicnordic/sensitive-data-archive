#!/bin/bash
set -e

# Ingest, verify and finalize a file large enough that every storage write takes
# longer than the idle_in_transaction_session_timeout set on the service roles in
# make_sda_credentials.sh. A service that holds a database transaction open across
# a storage write is killed by Postgres and the file never reaches "ready".
# The other tests use files of a few MB, which finish long before the timeout.
# Runs in the s3 and sync suites; the posix suite only runs the sftp upload test.

cd shared || true

FILE=large_file
SIZE_MB=512
trap 'rm -f "$FILE" "$FILE.c4gh"' EXIT

echo "creating a $SIZE_MB MB test file"
head -c "${SIZE_MB}M" </dev/urandom >"$FILE"
yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$FILE"
s3cmd -c s3cfg put "$FILE.c4gh" s3://test_dummy.org/

DEC_SHA=$(sha256sum "$FILE" | cut -d' ' -f 1)
DEC_MD5=$(md5sum "$FILE" | cut -d' ' -f 1)
ENC_SHA=$(sha256sum "$FILE.c4gh" | cut -d' ' -f 1)
ENC_MD5=$(md5sum "$FILE.c4gh" | cut -d' ' -f 1)
ENC_SIZE=$(stat -c %s "$FILE.c4gh")

## the inbox registers the file on upload, use its id as correlation id
RETRY_TIMES=0
CORRID=""
until [ -n "$CORRID" ]; do
    CORRID=$(psql -U postgres -h postgres -d sda -At -c "SELECT id FROM sda.files WHERE submission_file_path = '$FILE.c4gh';")
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for the upload to be registered"
        exit 1
    fi
    [ -n "$CORRID" ] || sleep 2
done

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

# wait_for_event <event> <retries>: poll the latest file event until it matches,
# fail early on "error" so a broken service does not eat the whole retry budget
wait_for_event() {
    RETRY_TIMES=0
    until [ "$(latest_event)" = "$1" ]; do
        event=$(latest_event)
        if [ "$event" = "error" ]; then
            echo "::error::File ended in state error while waiting for $1"
            exit 1
        fi
        echo "waiting for $1, current state: $event"
        RETRY_TIMES=$((RETRY_TIMES + 1))
        if [ "$RETRY_TIMES" -eq "$2" ]; then
            echo "::error::Time out while waiting for $1"
            exit 1
        fi
        sleep 5
    done
}

latest_event() {
    psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE file_id = '$CORRID' ORDER BY id DESC LIMIT 1;"
}

## ingest
encrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$ENC_SHA" \
        --arg md5 "$ENC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath "$FILE.c4gh" \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -fsS --connect-timeout 5 --max-time 20 -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body" | jq -e '.routed == true'

wait_for_event verified 60

# the archive holds the file without its crypt4gh header, which is stored in the database
archive_size=$(psql -U postgres -h postgres -d sda -At -c "SELECT archive_file_size FROM sda.files WHERE id = '$CORRID';")
header_size=$(psql -U postgres -h postgres -d sda -At -c "SELECT octet_length(decode(header, 'hex')) FROM sda.files WHERE id = '$CORRID';")
if [ "$archive_size" -ne $((ENC_SIZE - header_size)) ]; then
    echo "::error::Archived size $archive_size does not match uploaded size $ENC_SIZE minus header size $header_size"
    exit 1
fi

## finalize, including the backup copy
decrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$DEC_SHA" \
        --arg md5 "$DEC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

accession_payload=$(
    jq -r -c -n \
        --arg type accession \
        --arg user test@dummy.org \
        --arg filepath "$FILE.c4gh" \
        --arg accession_id EGAF74900000500 \
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

curl -fsS --connect-timeout 5 --max-time 20 -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$accession_body" | jq -e '.routed == true'

wait_for_event ready 60

echo "large file test completed successfully"
