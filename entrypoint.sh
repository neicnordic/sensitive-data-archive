#!/bin/bash

sed -i 's%MQ_USER%'${USER_NAME}'%g' /etc/rabbitmq/definitions.json
sed -i 's%MQ_PASSWORD_HASH%'${PASSWORD_HASH}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/advanced.config

exec /usr/local/bin/docker-entrypoint.sh "$@"
