#!/bin/sh

MQHOST="localhost:15672"
CEGA="localhost:15671"

if [ -f /.dockerenv ]; then
	MQHOST="mq:15671"
	CEGA="cegamq:15671"
fi

if [ ! "$(command -v jq)" ]; then
	if [ "$(id -u)" != 0 ]; then
		echo "jq is missing, unable to install it"
		exit 1
	fi

	apk add --no-cache curl jq
fi

RETRY_TIMES=0
until curl -s --cacert /tmp/certs/ca.crt -u test:test "https://$MQHOST/api/federation-links" |  jq -r '.[].status' | grep running; do
	echo "waiting for federation to start"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 30 ]; then
		echo "::error::Time out while waiting for federation to start"
		exit 1
	fi
	sleep 2
done


for r in completed error inbox verified; do
	curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$CEGA/api/queues/lega/v1.files.$r/contents"
done
curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$MQHOST/api/queues/sda/ingest/contents"

# Give some time to avoid confounders in logs
sleep 5

now=$(date -u +%s)

## test all shovels
for r in completed error inbox verified; do
	curl -s --cacert /tmp/certs/ca.crt -u test:test "https://$MQHOST/api/exchanges/sda/sda/publish" \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d "$(echo '{"vhost":"sda",
"name":"sda",
"properties":{
	"delivery_mode":2,
	"correlation_id":"CORRID",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"ROUTE",
"payload_encoding":"string",
"payload":"{\"route\":\"ROUTE\"}"
}' | sed -e "s/ROUTE/$r/" -e "s/CORRID/$now/")"
done

# check that message arrived in queue v1.files.inbox in cega MQ
for r in completed error inbox verified; do
	RETRY_TIMES=0
	until curl -s --cacert /tmp/certs/ca.crt -u test:test "https://$CEGA/api/queues/lega/v1.files.$r" | jq -r '.["messages_ready"]' | grep 1; do
		echo "waiting for message to be shoveled to files.$r"
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ "$RETRY_TIMES" -eq 30 ]; then
			echo "::error::Time out while waiting for message to be shoveled to files.$r"
			exit 1
		fi
		sleep 2
	done
done
sleep 5

## publish message on cega MQ to test the federation link
curl --cacert /tmp/certs/ca.crt -u test:test "https://$CEGA/api/exchanges/lega/localega.v1/publish" \
	-H 'Content-Type: application/json;charset=UTF-8' \
	-d '{"vhost":"lega",
"name":"localega.v1",
"properties": {
	"delivery_mode":2,
	"correlation_id":"9",
	"content_encoding":"UTF-8",
	"content_type":"application/json"
},
"routing_key":"files",
"payload_encoding":"string",
"payload":"{\"type\":\"ingest\",\"user\":\"test\",\"filepath\":\"test.file\",\"encrypted_checksums\":[{\"type\":\"sha256\",\"value\":\"abaa8521c6c0281523ac57a76afb6838f04d98b5976dd9737acd8b139b1b1ee6\"},{\"type\":\"md5\",\"value\":\"f3728c14bfce76ec5cfa262d8ce38fac\"}]}"
}'

# check that message arrived in queue ingest in MQ
sleep 5
RETRY_TIMES=0
until curl -s --cacert /tmp/certs/ca.crt -u test:test "https://$MQHOST/api/queues/sda/ingest" | jq -r '.["messages_ready"]' | grep 1; do
	echo "waiting for message to be moved to ingest"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 30 ]; then
		echo "::error::Time out while waiting for message to be moved to ingest"
		exit 1
	fi
	sleep 2
done

echo ""
echo "completed successfully"

for r in completed error inbox verified; do
	curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$CEGA/api/queues/lega/v1.files.$r/contents"
done
curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$MQHOST/api/queues/sda/ingest/contents"
