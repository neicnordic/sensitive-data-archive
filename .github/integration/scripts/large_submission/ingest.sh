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
payload=$(
	jq -c -n \
		--arg description "this is the key description" \
		--arg pubkey "$( base64 -w0 /shared/c4gh.pub.pem)" \
		'$ARGS.named'
)
token="$(cat /shared/token)"
resp="$(curl -s -k -L -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d "$payload" "http://api:8080/c4gh-keys/add")"
if [ "$resp" != "200" ]; then
	echo "Error when adding a public key hash, expected 200 got: $resp"
	exit 1
fi

stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

token="$(cat /shared/token)"

i=1
while [ $i -le $((submission_size)) ]; do
    user="test@dummy.org"
    inbox_path="inbox/test-file-$i.c4gh"
    fileID=$(psql -U postgres -h postgres -d sda -At -c "SELECT sda.register_file('$inbox_path', '$user');")
    if [ -z "$fileID" ]; then
        echo "register_file failed"
        exit 1
    fi

    # the API assumed that a file has a correlation ID so this needs to be done for now.
    psql -U postgres -h postgres -d sda -At -c "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, message) VALUES('$fileID', 'submitted', '$fileID', '$user', '{}');" >/dev/null

    ingest_payload=$(
        jq -r -c -n \
            --arg type ingest \
            --arg user "$user" \
            --arg filepath "$inbox_path" \
            '$ARGS.named'
    )

    curl -s -X POST "http://localhost:8090/file/ingest" \
        -H "Content-Type: application/json;charset=UTF-8" \
        -H "Authorization: Bearer $token" \
        -d "$ingest_payload" >/dev/null

    i=$((i + 1))
done

sleep 10
RETRY_TIMES=0
until [ "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/ingest | jq '.messages')" -eq 0 ]; do
    echo "waiting for messages to be processed"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for ingest to complete the work"
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

echo "test for ingesting $submission_size files completed successfully"