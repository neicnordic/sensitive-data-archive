#!/bin/bash

[[ -z "${MQ_USER}" ]] && echo 'Environment variable MQ_USER is empty' 1>&2 && exit 1
[[ -z "${MQ_PASSWORD_HASH}" ]] && echo 'Environment variable MQ_PASSWORD_HASH is empty' 1>&2 && exit 1
[[ -z "${CEGA_CONNECTION}" ]] && echo 'Environment variable CEGA_CONNECTION is empty' 1>&2 && exit 1

sed -i 's%MQ_USER%'${MQ_USER}'%g' /etc/rabbitmq/definitions.json
sed -i 's%MQ_PASSWORD_HASH%'${MQ_PASSWORD_HASH}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/definitions.json
sed -i 's%CEGA_CONNECTION%'${CEGA_CONNECTION}'%g' /etc/rabbitmq/advanced.config

exec "$@"
