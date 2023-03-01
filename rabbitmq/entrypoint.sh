#!/bin/sh

[ -z "${MQ_USER}" ] && echo 'Environment variable MQ_USER is empty' 1>&2 && exit 1
[ -z "${MQ_PASSWORD_HASH}" ] && echo 'Environment variable MQ_PASSWORD_HASH is empty' 1>&2 && exit 1

if [ -n "${NOTLS+x}" ]; then
	echo "Disabeling TLS"
	unset MQ_SERVER_CERT
	unset MQ_SERVER_KEY
	unset MQ_CA
	cat >"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
		listeners.tcp.default = 5672
		disk_free_limit.absolute = 1GB
		management.tcp.port = 15672
		load_definitions = /var/lib/rabbitmq/definitions.json
		default_vhost = ${MQ_VHOST:-/}
	EOF
else
	if [ -e "${MQ_SERVER_CERT}" ] || [ -e "${MQ_SERVER_KEY}" ]; then
		echo "Enabeling TLS"
		cat >"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
			listeners.ssl.default = 5671
			ssl_options.certfile = ${MQ_SERVER_CERT}
			ssl_options.keyfile = ${MQ_SERVER_KEY}
			ssl_options.versions.1 = tlsv1.2
			disk_free_limit.absolute = 1GB
			management.ssl.port = 15672
			management.ssl.certfile   = ${MQ_SERVER_CERT}
			management.ssl.keyfile    = ${MQ_SERVER_KEY}
			load_definitions = /var/lib/rabbitmq/definitions.json
			default_vhost = ${MQ_VHOST:-/}
		EOF

		if [ -e "${MQ_CA}" ] && [ "${MQ_VERIFY}" = "verify_peer" ]; then
			cat >>"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
				ssl_options.verify = verify_peer
				ssl_options.fail_if_no_peer_cert = true
				ssl_options.cacertfile = ${MQ_CA}
			EOF
		fi

	else
		echo 'No server certificates found, shuting down.' 1>&2 && exit 1
	fi

	if [ -e "${MQ_SERVER_CERT}" ] && [ -e "${MQ_SERVER_KEY}" ]; then
		cat >>"/var/lib/rabbitmq/rabbitmq.conf" <<-EOF
			listeners.tcp  = none
		EOF
	fi

	chmod 600 "/var/lib/rabbitmq/rabbitmq.conf"
fi

if [ -n "${MQ_VHOST}" ]; then
	MQ_SHOVEL_VHOST="/${MQ_VHOST}"
fi

if [ -n "${CEGA_CONNECTION}" ]; then
	cat >"/var/lib/rabbitmq/definitions.json" <<-EOF
		{
			"users": [
				{
					"name": "${MQ_USER}",
					"password_hash": "${MQ_PASSWORD_HASH}",
					"hashing_algorithm": "rabbit_password_hashing_sha256",
					"tags": "administrator"
				}
			],
			"vhosts": [
				{
					"name": "${MQ_VHOST:-/}"
				}
			],
			"permissions": [
				{
					"user": "${MQ_USER}",
					"vhost": "${MQ_VHOST:-/}",
					"configure": ".*",
					"write": ".*",
					"read": ".*"
				}
			],
			"parameters": [
				{
					"component": "shovel",
					"name": "to_cega",
					"value": {
						"ack-mode": "on-confirm",
						"dest-add-forward-headers": true,
						"dest-exchange": "localega.v1",
						"dest-protocol": "amqp091",
						"dest-uri": "${CEGA_CONNECTION}",
						"reconnect-delay": 5,
						"src-delete-after": "never",
						"src-exchange": "to_cega",
						"src-exchange-key": "#",
						"src-protocol": "amqp091",
						"src-uri": "amqp://${MQ_SHOVEL_VHOST:-}"
					},
					"vhost": "${MQ_VHOST:-/}"
				},
				{
					"component": "shovel",
					"name": "cega_inbox",
					"value": {
						"ack-mode": "on-confirm",
						"dest-exchange": "to_cega",
						"dest-exchange-key": "files.inbox",
						"dest-protocol": "amqp091",
						"dest-uri": "amqp://${MQ_SHOVEL_VHOST:-}",
						"src-delete-after": "never",
						"src-protocol": "amqp091",
						"src-queue": "inbox",
						"src-uri": "amqp://${MQ_SHOVEL_VHOST:-}"
					},
					"vhost": "${MQ_VHOST:-/}"
				},
				{
					"component": "shovel",
					"name": "cega_completion",
					"value": {
						"ack-mode": "on-confirm",
						"dest-exchange": "to_cega",
						"dest-exchange-key": "files.completed",
						"dest-protocol": "amqp091",
						"dest-uri": "amqp://${MQ_SHOVEL_VHOST:-}",
						"src-delete-after": "never",
						"src-protocol": "amqp091",
						"src-queue": "completed",
						"src-uri": "amqp://${MQ_SHOVEL_VHOST:-}"
					},
					"vhost": "${MQ_VHOST:-/}"
				},
				{
					"component": "shovel",
					"name": "cega_verified",
					"value": {
						"ack-mode": "on-confirm",
						"dest-exchange": "to_cega",
						"dest-exchange-key": "files.verified",
						"dest-protocol": "amqp091",
						"dest-uri": "amqp://${MQ_SHOVEL_VHOST:-}",
						"src-delete-after": "never",
						"src-protocol": "amqp091",
						"src-queue": "verified",
						"src-uri": "amqp://${MQ_SHOVEL_VHOST:-}"
					},
					"vhost": "${MQ_VHOST:-/}"
				},
				{
					"component": "shovel",
					"name": "cega_error",
					"value": {
						"ack-mode": "on-confirm",
						"dest-exchange": "to_cega",
						"dest-exchange-key": "files.error",
						"dest-protocol": "amqp091",
						"dest-uri": "amqp://${MQ_SHOVEL_VHOST:-}",
						"src-delete-after": "never",
						"src-protocol": "amqp091",
						"src-queue": "error",
						"src-uri": "amqp://${MQ_SHOVEL_VHOST:-}"
					},
					"vhost": "${MQ_VHOST:-/}"
				},
				{
					"component": "federation-upstream",
					"name": "CEGA-files",
					"value": {
						"ack-mode": "on-confirm",
						"queue": "v1.files",
						"trust-user-id": false,
						"uri": "${CEGA_CONNECTION}"
					},
					"vhost": "${MQ_VHOST:-/}"
				}
			],
			"policies": [
				{
					"vhost": "${MQ_VHOST:-/}",
					"name": "CEGA-files",
					"pattern": "files",
					"apply-to": "queues",
					"priority": 0,
					"definition": {
						"federation-upstream": "CEGA-files"
					}
				}
			],
			"queues": [
				{
					"name": "archived",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "backup",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "completed",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "error",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "files",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "inbox",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "ingest",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "mappings",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "accessionIDs",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "verified",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				}
			],
			"exchanges": [
				{
					"name": "to_cega",
					"vhost": "${MQ_VHOST:-/}",
					"type": "topic",
					"durable": true,
					"auto_delete": false,
					"internal": false,
					"arguments": {}
				},
				{
					"name": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"type": "topic",
					"durable": true,
					"auto_delete": false,
					"internal": false,
					"arguments": {}
				}
			],
			"bindings": [
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "archived",
					"routing_key": "archived"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "accessionIDs",
					"routing_key": "accessionIDs"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "backup",
					"routing_key": "backup"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "completed",
					"routing_key": "completed"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "error",
					"routing_key": "error"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "files",
					"routing_key": "files"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "inbox",
					"routing_key": "inbox"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "ingest",
					"routing_key": "ingest"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "mappings",
					"routing_key": "mappings"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "verified",
					"routing_key": "verified"
				}
			]
		}
	EOF

else
	cat >"/var/lib/rabbitmq/definitions.json" <<-EOF
		{
			"users": [
				{
					"name": "${MQ_USER}",
					"password_hash": "${MQ_PASSWORD_HASH}",
					"hashing_algorithm": "rabbit_password_hashing_sha256",
					"tags": "administrator"
				}
			],
			"vhosts": [
				{
					"name": "${MQ_VHOST:-/}"
				}
			],
			"permissions": [
				{
					"user": "${MQ_USER}",
					"vhost": "${MQ_VHOST:-/}",
					"configure": ".*",
					"write": ".*",
					"read": ".*"
				}
			],
			"policies": [],
			"queues": [
				{
					"name": "archived",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "backup",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "completed",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "error",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "files",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "inbox",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "ingest",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "mappings",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "accessionIDs",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				},
				{
					"name": "verified",
					"vhost": "${MQ_VHOST:-/}",
					"durable": true,
					"auto_delete": false,
					"arguments": {}
				}
			],
			"exchanges": [
				{
					"name": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"type": "topic",
					"durable": true,
					"auto_delete": false,
					"internal": false,
					"arguments": {}
				}
			],
			"bindings": [
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "archived",
					"routing_key": "archived"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "accessionIDs",
					"routing_key": "accessionIDs"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "backup",
					"routing_key": "backup"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "completed",
					"routing_key": "completed"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "error",
					"routing_key": "error"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "files",
					"routing_key": "files"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "inbox",
					"routing_key": "inbox"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "ingest",
					"routing_key": "ingest"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "mappings",
					"routing_key": "mappings"
				},
				{
					"source": "sda",
					"vhost": "${MQ_VHOST:-/}",
					"destination_type": "queue",
					"arguments": {},
					"destination": "verified",
					"routing_key": "verified"
				}
			]
		}
	EOF
fi

chmod 600 "/var/lib/rabbitmq/definitions.json"

cookie_file="/var/lib/rabbitmq/.erlang.cookie"
if [ -f "$cookie_file" ]; then
    chmod g-rw "$cookie_file"
fi

exec "$@"
