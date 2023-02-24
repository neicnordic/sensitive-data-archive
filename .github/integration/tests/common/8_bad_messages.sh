#!/bin/bash

#
# Submit some messages that will trigger various failures. Do this
# before the "real" work to verify that these failures are not top of
# queue and handled again and again.
#
if [ "$STORAGETYPE" = s3notls ] || [ "$STORAGETYPE" = s3notlsheader ]; then
    exit 0
fi

cd dev_utils || exit 1

function check_move_to_error_queue() {
	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	echo "$now"
	RETRY_TIMES=0
	echo
	echo "Waiting for msg containing \"""$1""\" to move to error queue."
	until curl --cacert certs/ca.pem  -u test:test 'https://localhost:15672/api/queues/test/error/get' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d '{"count":1,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' 2>&1 | grep -q "$1"; do
		printf '%s' "."
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ $RETRY_TIMES -eq 30 ]; then
			echo "::error::Time out while waiting for msg to move to error queue, logs:"
			for k in intercept ingest verify backup finalize mapper; do
				echo
				echo "$k"
				echo
				docker logs --since="$now" "$k"
			done
			exit 1
		fi
		sleep 2
	done
	echo
	echo "Message with \"""$1""\" moved to error queue."
	echo
}

for routingkey in files ingest archived accessionIDs backup mappings; do
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"1",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"'"$routingkey"'",
						"payload_encoding":"string",
						"payload":"{I give you bad json!}"
					}'

check_move_to_error_queue "I give you bad json"

done

for routingkey in files ingest archived accessionIDs backup mappings; do
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"1",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"'"$routingkey"'",
						"payload_encoding":"string",
						"payload":"{ \"json\":\"yes, but not sda\" }"
					}'

check_move_to_error_queue "yes, but not sda"

done

# intercept: test wrong type of message
curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
	-H 'Content-Type: application/json;charset=UTF-8' \
	--data-binary '{
					"vhost":"test",
					"name":"sda",
					"properties":{
					"delivery_mode":2,
						"correlation_id":"1",
						"content_encoding":"UTF-8",
						"content_type":"application/json"
					},
					"routing_key":"files",
					"payload_encoding":"string",
					"payload":"{ \"type\":\"not the right one\" }"
				}'

check_move_to_error_queue "Don't know what schema to use"

# Cleanup queues
curl --cacert certs/ca.pem  -u test:test -X DELETE 'https://localhost:15672/api/queues/test/error/contents'
