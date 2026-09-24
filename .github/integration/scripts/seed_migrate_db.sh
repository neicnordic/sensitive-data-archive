#!/bin/bash
# Restore the old-schema fixture into an empty PGDATA so that the migrate
# service in postgres.yml starts on it and runs the migration scripts.
set -eo pipefail

if [ "$(id -u)" = '0' ]; then
	# docker creates the volume directory as root
	mkdir -p "$PGDATA"
	chown postgres "$PGDATA"
	exec su postgres -s /bin/bash "$0" "$@"
fi

if [ -s "$PGDATA/PG_VERSION" ]; then
	echo "$PGDATA is already initialised, not seeding"
	exit 0
fi

initdb --username=postgres --pwfile=<(printf "%s\n" "$POSTGRES_PASSWORD")
pg_ctl -D "$PGDATA" -o "-c listen_addresses=''" -w start

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres -c "CREATE DATABASE sda;"
psql -v ON_ERROR_STOP=1 --username postgres --dbname sda --quiet -f "$1"

pg_ctl -D "$PGDATA" -m fast -w stop
