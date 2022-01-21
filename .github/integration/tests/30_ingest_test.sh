#!/bin/bash

cd .github/integration || exit 1

chmod 600 certs/client-key.pem

count=1
file=file.$RANDOM.c4gh

docker cp file.raw.c4gh ingest:/inbox/$file

curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.inbox/contents'
curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.verified/contents'
curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.completed/contents'

# Give some time to avoid confounders in logs
sleep 5

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

md5sum=$(md5sum file.raw.c4gh | cut -d' ' -f 1)
sha256sum=$(sha256sum file.raw.c4gh | cut -d' ' -f 1)

C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
export C4GH_PASSPHRASE

decsha256sum=$(cat file.raw.sha256 | cut -d' ' -f 1)
decmd5sum=$(cat file.raw.md5 | cut -d' ' -f 1)
decryptedfilesize=$(cat file.raw.stats | cut -d' ' -f 1)

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
	-H 'Content-Type: application/json;charset=UTF-8' \
	--data-binary "$(echo '{"vhost":"test",
"name":"sda",
"properties":{
	"delivery_mode":2,
	"correlation_id":"CORRID",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"inbox",
"payload_encoding":"string",
"payload":"{
	\"operation\":\"upload\",
	\"user\":\"test\",
	\"filepath\":\"FILENAME\",
	\"encrypted_checksums\":[
		{\"type\":\"sha256\",\"value\":\"SHA256SUM\"},
		{\"type\":\"md5\",\"value\":\"MD5SUM\"}
	]
}"
}' | sed -e "s/FILENAME/$file/" -e "s/MD5SUM/${md5sum}/" -e "s/SHA256SUM/${sha256sum}/" -e "s/CORRID/$count/")"

# check that message arrived in queue v1.files.inbox in cega MQ
RETRY_TIMES=0
until curl -s --cacert certs/ca.pem -u test:test 'https://localhost:15671/api/queues/lega/v1.files.inbox' | jq -r '.["messages_ready"]' | grep 1; do
	echo "waiting for message to be shoveled to files.inbox"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 20 ]; then
		echo "::error::Time out while waiting for message to be shoveled to files.inbox"
		exit 1
	fi
	sleep 1
done

sleep 5

curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15671/api/exchanges/lega/localega.v1/publish' \
	-H 'Content-Type: application/json;charset=UTF-8' \
	--data-binary "$(echo '{
"vhost":"lega",
"name":"localega.v1",
"properties":{
	"delivery_mode":2,
	"correlation_id":"CORRID",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"files",
"payload_encoding":"string",
"payload":"{
	\"type\":\"ingest\",
	\"user\":\"test\",
	\"filepath\":\"FILENAME\",
	\"encrypted_checksums\":[
		{
			\"type\":\"sha256\",
			\"value\":\"SHA256SUM\"
		},
		{
			\"type\":\"md5\",
			\"value\":\"MD5SUM\"
		}
	]
}"
}' | sed -e "s/FILENAME/$file/" -e "s/MD5SUM/${md5sum}/" -e "s/SHA256SUM/${sha256sum}/" -e "s/CORRID/$count/")"

# check that message arrived in queue v1.files.verified in cega MQ
RETRY_TIMES=0
until curl -s --cacert certs/ca.pem -u test:test 'https://localhost:15671/api/queues/lega/v1.files.verified' | jq -r '.["messages_ready"]' | grep 1; do
	echo "waiting for message to be shoveled to files.verified"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 20 ]; then
		echo "::error::Time out while waiting for message to be shoveled to files.verified"
		exit 1
	fi
	sleep 1
done

sleep 5

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
access=$(printf "EGAF%05d%06d" "$count" "$RANDOM")

# Publish accession id
curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15671/api/exchanges/lega/localega.v1/publish' \
	-H 'Content-Type: application/json;charset=UTF-8' \
	--data-binary "$(echo '{
"vhost":"lega",
"name":"localega.v1",
"properties":{
	"delivery_mode":2,
	"correlation_id":"CORRID",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"files",
"payload_encoding":"string",
"payload":"{
	\"type\":\"accession\",
	\"user\":\"test\",
	\"filepath\":\"FILENAME\",
	\"accession_id\":\"ACCESSIONID\",
	\"decrypted_checksums\":[
		{
			\"type\":\"sha256\",
			\"value\":\"DECSHA256SUM\"
		},
		{
			\"type\":\"md5\",
			\"value\":\"DECMD5SUM\"
		}
	]
}"
}' | sed -e "s/FILENAME/$file/" -e "s/DECMD5SUM/${decmd5sum}/" -e "s/DECSHA256SUM/${decsha256sum}/" -e "s/ACCESSIONID/${access}/" -e "s/CORRID/$count/")"

# check that message arrived in queue v1.files.verified in cega MQ
RETRY_TIMES=0
until curl -s --cacert certs/ca.pem -u test:test 'https://localhost:15671/api/queues/lega/v1.files.completed' | jq -r '.["messages_ready"]' | grep 1; do
	echo "waiting for message to be shoveled to files.completed"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 20 ]; then
		echo "::error::Time out while waiting for message to be shoveled to files.completed"
		exit 1
	fi
	sleep 1
done

sleep 5

dataset=$(printf "EGAD%011d" "$RANDOM")

echo "map dataset"
# Map dataset ids
curl --cacert certs/ca.pem -s -u test:test 'https://localhost:15671/api/exchanges/lega/localega.v1/publish' \
	-H 'Content-Type: application/json;charset=UTF-8' \
	--data-binary "$(echo '{
"vhost":"lega",
"name":"localega.v1",
"properties":{
	"delivery_mode":2,
	"correlation_id":"CORRID",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"files",
"payload_encoding":"string",
"payload":"{
	\"type\":\"mapping\",
	\"dataset_id\":\"DATASET\",
	\"accession_ids\":[\"ACCESSIONID\"]}"
}' | sed -e "s/DATASET/$dataset/" -e "s/ACCESSIONID/$access/" -e "s/CORRID/$count/")"

RETRY_TIMES=0
dbcheck=''

until [ "${#dbcheck}" -ne 0 ]; do

	dbcheck=$(docker run --rm --name client --network integration_default -v "$PWD/certs:/certs" \
		-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
		neicnordic/pg-client:latest postgresql://lega_out:lega_out@db:5432/lega \
		-t -c "SELECT * from local_ega_ebi.file_dataset where dataset_id='$dataset' and file_id='$access'")

	if [ "${#dbcheck}" -eq 0 ]; then

		sleep 2
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ "$RETRY_TIMES" -eq 30 ]; then
			echo "Mappings failed"
			exit 1
		fi
	fi
done

echo ""
echo "completed successfully"
