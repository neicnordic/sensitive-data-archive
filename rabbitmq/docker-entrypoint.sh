#!/usr/bin/env bash
set -eo pipefail

# allow the container to be started with `--user`
if [[ "$1" == rabbitmq* ]] && [ "$(id -u)" = '0' ]; then
	if [ "$1" = 'rabbitmq-server' ]; then
		find /var/lib/rabbitmq \! -user rabbitmq -exec chown rabbitmq '{}' +
	fi

	exec su-exec rabbitmq "${BASH_SOURCE[0]}" "$@"
fi

RABBITMQ_DEFAULT_USER="${RABBITMQ_DEFAULT_USER:-guest}"
RABBITMQ_DEFAULT_PASS="${RABBITMQ_DEFAULT_PASS:-guest}"

sed -e "s/RABBITMQ_DEFAULT_USER/$RABBITMQ_DEFAULT_USER/" -e "s/RABBITMQ_DEFAULT_PASS/$RABBITMQ_DEFAULT_PASS/" \
	/etc/rabbitmq/definitions.json >/var/lib/rabbitmq/definitions.json

echo "load_definitions = /var/lib/rabbitmq/definitions.json" >"/var/lib/rabbitmq/rabbitmq.conf"

if [ -e "${RABBITMQ_SERVER_CERT}" ] && [ -e "${RABBITMQ_SERVER_KEY}" ]; then
	echo "Enabeling TLS"
	cat >>"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
		listeners.tcp  = none
		listeners.ssl.default = 5671
		ssl_options.certfile = ${RABBITMQ_SERVER_CERT}
		ssl_options.keyfile = ${RABBITMQ_SERVER_KEY}
		ssl_options.versions.1 = tlsv1.2
		disk_free_limit.absolute = 1GB
		management.ssl.port = 15671
		management.ssl.certfile = ${RABBITMQ_SERVER_CERT}
		management.ssl.keyfile = ${RABBITMQ_SERVER_KEY}
	EOF

	if [ -e "${RABBITMQ_SERVER_CACERT}" ] && [ "${RABBITMQ_SERVER_VERIFY}" = "verify_peer" ]; then
		cat >>"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
			ssl_options.verify = verify_peer
			ssl_options.fail_if_no_peer_cert = true
			ssl_options.cacertfile = ${RABBITMQ_SERVER_CACERT}
		EOF
	fi
fi

if [ -n "$CEGA_CONNECTION" ]; then
	echo "Enabling federation links"
	sed "s|CEGA_CONNECTION|$CEGA_CONNECTION|g" /etc/rabbitmq/federation.json >/var/lib/rabbitmq/federation.json
	sleep 30 && rabbitmqctl import_definitions /var/lib/rabbitmq/federation.json &
	chmod 600 "/var/lib/rabbitmq/federation.json" 
fi

if [ -n "$BP_SYNC" ]; then
	echo "Enabling sync queues and shovels"
	sleep 30 && rabbitmqctl import_definitions /etc/rabbitmq/sync.json &
fi

# This is needed for the streams to work properly
cat >/var/lib/rabbitmq/advanced.config<<-EOF
[
	{rabbit, [
		{consumer_timeout, ${RABBITMQ_CONSUMER_TIMEOUT:-14400000}},
		{default_consumer_prefetch, {false,1}}
		]
	}
].
EOF

chmod 600 "/var/lib/rabbitmq/advanced.config"
chmod 600 "/var/lib/rabbitmq/rabbitmq.conf"
chmod 600 "/var/lib/rabbitmq/definitions.json"

exec "$@"
