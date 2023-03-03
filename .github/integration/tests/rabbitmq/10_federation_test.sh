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
until curl -s --cacert /tmp/certs/ca.crt -u guest:guest "https://$MQHOST/api/federation-links" | jq -r '.[].status' | grep running; do
	echo "waiting for federation to start"
	RETRY_TIMES=$((RETRY_TIMES + 1))
	if [ "$RETRY_TIMES" -eq 30 ]; then
		echo "::error::Time out while waiting for federation to start"
		exit 1
	fi
	sleep 2
done

for r in completed errors inbox verified; do
	curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$CEGA/api/queues/cega/$r/contents"
done
curl -s --cacert /tmp/certs/ca.crt -X DELETE -u guest:guest "https://$MQHOST/api/queues/sda/ingest/contents"

# Give some time to avoid confounders in logs
sleep 5

## test all shovels
i=0
for r in completed error inbox verified; do
	i=+1

	properties=$(
		jq -c -n \
			--argjson delivery_mode 2 \
			--arg correlation_id "$i" \
			--arg content_encoding UTF-8 \
			--arg content_type application/json \
			'$ARGS.named'
	)

	payload_string=$(
		jq -r -c -n \
			--arg operation upload \
			--arg user test \
			--arg route "$r"
	)

	request_body=$(
		jq -c -n \
			--arg vhost test \
			--arg name sda \
			--argjson properties "$properties" \
			--arg routing_key "$r" \
			--arg payload_encoding string \
			--arg payload "$payload_string" \
			'$ARGS.named'
	)

	curl -s --cacert /tmp/certs/ca.crt -u guest:guest "https://$MQHOST/api/exchanges/sda/sda/publish" \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d "$request_body"
done

# check that message arrived in queue v1.files.inbox in cega MQ
for r in completed errors inbox verified; do
	RETRY_TIMES=0
	until curl -s --cacert /tmp/certs/ca.crt -u test:test "https://$CEGA/api/queues/cega/$r" | jq -r '.["messages_ready"]' | grep 1; do
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
properties=$(
	jq -c -n \
		--argjson delivery_mode 2 \
		--arg correlation_id "999" \
		--arg content_encoding UTF-8 \
		--arg content_type application/json \
		'$ARGS.named'
)

encrypted_checksums=$(
	jq -c -n \
		--arg sha256 "abaa8521c6c0281523ac57a76afb6838f04d98b5976dd9737acd8b139b1b1ee6" \
		--arg md5 "f3728c14bfce76ec5cfa262d8ce38fac" \
		'$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

payload_string=$(
	jq -r -c -n \
		--arg type ingest \
		--arg user test \
		--arg filepath test/file.c4gh \
		--argjson encrypted_checksums "$encrypted_checksums" \
		'$ARGS.named|@base64'
)

request_body=$(
	jq -c -n \
		--arg vhost test \
		--arg name sda \
		--argjson properties "$properties" \
		--arg routing_key "ingest" \
		--arg payload_encoding base64 \
		--arg payload "$payload_string" \
		'$ARGS.named'
)

curl --cacert /tmp/certs/ca.crt -u test:test "https://$CEGA/api/exchanges/cega/localega/publish" \
	-H 'Content-Type: application/json;charset=UTF-8' \
	-d "$request_body"

# check that message arrived in queue ingest in MQ
sleep 5
RETRY_TIMES=0
until curl -s --cacert /tmp/certs/ca.crt -u guest:guest "https://$MQHOST/api/queues/sda/ingest" | jq -r '.["messages_ready"]' | grep 1; do
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

for r in completed errors inbox verified; do
	curl -s --cacert /tmp/certs/ca.crt -X DELETE -u test:test "https://$CEGA/api/queues/cega/$r/contents"
done
curl -s --cacert /tmp/certs/ca.crt -X DELETE -u guest:guest "https://$MQHOST/api/queues/sda/ingest/contents"
