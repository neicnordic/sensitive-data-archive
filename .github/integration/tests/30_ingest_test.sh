#!/bin/bash

cd .github/integration || exit 1

chmod 600 certs/client-key.pem

count=1

curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.inbox/contents'
curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.verified/contents'
curl -s --cacert certs/ca.pem -X DELETE -u test:test 'https://localhost:15671/api/queues/lega/v1.files.completed/contents'

# Give some time to avoid confounders in logs
sleep 5

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

file=$RANDOM
md5sum=$(md5sum certs/client-key.pem | cut -d' ' -f 1)
sha256sum=$(sha256sum certs/client-key.pem | cut -d' ' -f 1)

## test inbox shovel
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

# check that message arrived in queue ingest in MQ
RETRY_TIMES=0
until curl -s --cacert certs/ca.pem -u test:test 'https://localhost:15671/api/queues/lega/v1.files.verified' | jq -r '.["messages_ready"]' | grep 1; do
	echo "waiting for message to be shoveled to files.verified"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 20 ]; then
		echo "::error::Time out while waiting for message to be moved to ingest"
		exit 1
	fi
	sleep 1
done

echo ""
echo "completed successfully"
