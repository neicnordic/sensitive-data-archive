#!/bin/sh
set -ex

MQHOST="localhost:15672"
if [ -f /.dockerenv ]; then
	MQHOST="mq:15671"
fi

timeout=$(curl -s --cacert /tmp/certs/ca.crt -u guest:guest "https://$MQHOST/api/queues/sda/from_cega" | jq '."consumer_details"[]."consumer_timeout"')
if [ "$timeout" -ne 1000 ]; then
    echo "active timeout is wrong, expected: 1000, actual: $timeout"
    exit 1
fi

echo "configuration test completed successfully"
