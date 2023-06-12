#!/bin/sh

for c in s3cmd jq
do
    if ! command -v $c
    then
        echo "$c could not be found"
        exit 1
    fi
done

# we need this certificate to be of 600 to work with db connection
chmod 600 certs/client-key.pem

FILE="dummy_data.c4gh"
SHA="5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578"
MD5="b60fa2486b121bed8d566bacec987e0d"

if [ "$1" != "" ]; then
    SHA=$(sha256sum "$1" | awk '{print $1;}')
    MD5=$(md5sum "$1" | awk '{print $1;}')
    FILE="$(realpath "$1")"
fi

file=test/$(basename "$FILE")

cd "$(dirname "$0")" || exit

s3cmd -c s3cmd.conf put "$FILE" s3://inbox/"$file"

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
-H 'Content-Type: application/json;charset=UTF-8' \
--data-binary '{"vhost":"test","name":"sda","properties":{"delivery_mode":2,"correlation_id":"1","content_encoding":"UTF-8","content_type":"application/json"},"routing_key":"files","payload_encoding":"string","payload":"{\"type\":\"ingest\",\"user\":\"test\",\"filepath\":\"'"$file"'\",\"encrypted_checksums\":[{\"type\":\"sha256\",\"value\":\"'"$SHA"'\",\"type\":\"md5\",\"value\":\"'"$MD5"'\"}]}"}'

RETRY_TIMES=0
until docker logs --since 30s ingest 2>&1 | grep "File marked as archived"
do 
    echo "waiting for ingestion to complete"
    RETRY_TIMES=$((RETRY_TIMES+1));
    if [ $RETRY_TIMES -eq 30 ]; then
        echo "Ingest failed"
        exit 1
    fi
    sleep 10;
done

RETRY_TIMES=0
until docker logs --since 30s verify 2>&1 | grep "File marked completed"
do
    echo "waiting for verification to complete"
    RETRY_TIMES=$((RETRY_TIMES+1));
    if [ $RETRY_TIMES -eq 30 ]; then
        echo "Verification failed"
        exit 1
    fi
    sleep 10;
done

RETRY_TIMES=0
until docker logs --since 30s verify 2>&1 | grep "Removed file from inbox"
do
    echo "waiting for removal of file from inbox"
    RETRY_TIMES=$((RETRY_TIMES+1));
    if [ $RETRY_TIMES -eq 20 ]; then
        echo "check file removed from inbox failed"
        exit 1
    fi
    sleep 10;
done

count=$(s3cmd -c s3cmd.conf ls s3://inbox/"$file" | wc -l)

if [ "${count}" -gt 0 ]; then
    echo "File has not been removed after ingest from inbox"
    exit 1
else
    echo "File has been removed after ingest from inbox, continuing ..."
fi

message=$(curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/queues/test/verified/get' \
-H 'Content-Type: application/json;charset=UTF-8' \
-d '{"count":1,"ackmode":"ack_requeue_true","encoding":"auto","truncate":50000}')

decrypted_sha=$(echo "$message" | jq '.[0].payload | fromjson.decrypted_checksums[0].value' | cut -d '"' -f 2)
decrypted_md5=$(echo "$message" | jq '.[0].payload | fromjson.decrypted_checksums[1].value' | cut -d '"' -f 2)
file=$(echo "$message" | jq '.[0].payload | fromjson.filepath' | cut -d '"' -f 2)

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
-H 'Content-Type: application/json;charset=UTF-8' \
--data-binary '{"vhost":"test","name":"sda","properties":{"delivery_mode":2,"correlation_id":"1","content_encoding":"UTF-8","content_type":"application/json"},"routing_key":"files","payload_encoding":"string","payload":"{\"type\":\"accession\",\"user\":\"test\",\"filepath\":\"'"$file"'\",\"accession_id\":\"EGAF00123456789\",\"decrypted_checksums\":[{\"type\":\"sha256\",\"value\":\"'"$decrypted_sha"'\"},{\"type\":\"md5\",\"value\":\"'"$decrypted_md5"'\"}]}"}'

RETRY_TIMES=0
until docker logs finalize 2>&1 | grep "Mark ready"
do
    echo "waiting for finalize to complete"
    RETRY_TIMES=$((RETRY_TIMES+1));
    if [ $RETRY_TIMES -eq 30 ]; then
        echo "Finalize failed"
        exit 1
    fi
    sleep 10;
done

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
-H 'Content-Type: application/json;charset=UTF-8' \
--data-binary '{"vhost":"test","name":"sda","properties":{"delivery_mode":2,"correlation_id":"1","content_encoding":"UTF-8","content_type":"application/json"},"routing_key":"files","payload_encoding":"string","payload":"{\"type\":\"mapping\",\"dataset_id\":\"EGAD00123456789\",\"accession_ids\":[\"EGAF00123456789\"]}"}'

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
-H 'Content-Type: application/json;charset=UTF-8' \
--data-binary '{"vhost":"test","name":"sda","properties":{"delivery_mode":2,"correlation_id":"1","content_encoding":"UTF-8","content_type":"application/json"},"routing_key":"files","payload_encoding":"string","payload":"{\"type\":\"release\",\"dataset_id\":\"EGAD00123456789\"}"}'

dataset=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://lega_out:lega_out@db:5432/lega \
-c "SELECT * from local_ega_ebi.file_dataset" | grep EGAD00123456789)
if [ ${#dataset} -eq 0 ]; then
    echo "Mappings failed"
    exit 1
else
    echo "Success"
fi
