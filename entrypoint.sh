#!/bin/bash

sed -i 's%MQ_USER%'${MQ_USER}'%g' /etc/rabbitmq/definitions.json
sed -i 's%MQ_PASSWORD_HASH%'${MQ_PASSWORD_HASH}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/advanced.config

exec "$@"
