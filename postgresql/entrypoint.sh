#!/usr/bin/env bash
set -Eeo pipefail

# allow the container to be started with `--user`
if [[ "$1" == postgres* ]] && [ "$(id -u)" = '0' ]; then
	if [ "$1" = 'postgres' ]; then
		find /var/lib/postgresql \! -user postgres -exec chown postgres '{}' +
	fi

	exec su postgres "${BASH_SOURCE[0]}" "$@"
fi

migrate() {
	runmigration=1
	migfile="${PGDATA}/migrations.$$"

	echo
	echo "Running schema migrations"
	echo

	POSTGRES_USER=postgres
	export PGPASSWORD="${PGPASSWORD:-$POSTGRES_PASSWORD}"

	temp_server_start "$@"
	sleep 2

	while [ 0 -lt "$runmigration" ]; do

		for f in migratedb.d/*.sql; do
			echo "Running migration script $f"
			psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --no-psqlrc --dbname "${POSTGRES_DB:-sda}" -f "$f"
			echo "Done"
		done 2>&1 | tee "$migfile"

		if grep -F 'Doing migration from' "$migfile"; then
			runmigration=1
			echo
			echo "At least one change occured, running migrations scripts again"
			echo
		else
			runmigration=0
			echo
			echo "No changes registered, done with migrations"
			echo
		fi

		rm -f "$migfile"
	done

	pg_ctl -D "$PGDATA" -w stop

	unset PGPASSWORD
}

temp_server_start() {
	if [ "$1" = 'postgres' ]; then
		shift
	fi

	# internal start of server in order to allow setup using psql client
	# does not listen on external TCP/IP and waits until start finishes

	PGUSER="${POSTGRES_USER:-postgres}" \
		pg_ctl -D "$PGDATA" \
		-o "-c listen_addresses='' -p 5432" \
		-w start
}

setup_hba_conf() {
	if [ -f "$POSTGRES_SERVER_CERT" ] && [ -f "$POSTGRES_SERVER_KEY" ] && [ -f "$POSTGRES_SERVER_CACERT" ]; then
		echo "Enabling TLS"
		# - Enforcing SSL communication for all connections
		cat >"$PGDATA/pg_hba.conf" <<-EOF
			# TYPE    DATABASE USER ADDRESS      METHOD
			local     all      all               scram-sha-256
			hostnossl all      all  0.0.0.0/0    reject
			hostssl   all      all  127.0.0.1/32 scram-sha-256
			hostssl   all      all  ::1/128      scram-sha-256
			hostssl   all      all  0.0.0.0/0    scram-sha-256 clientcert=${POSTGRES_VERIFY_PEER:-verify-ca}
		EOF

		cat >>"$PGDATA/postgresql.conf" <<-EOF
			ssl = on
			ssl_cert_file = '${POSTGRES_SERVER_CERT}'
			ssl_key_file = '${POSTGRES_SERVER_KEY}'
			ssl_ca_file = '${POSTGRES_SERVER_CACERT}'
		EOF
	else
		cat >"$PGDATA/pg_hba.conf" <<-EOF
			# TYPE    DATABASE USER ADDRESS      METHOD
			local     all      all               scram-sha-256
			hostnossl all      all  127.0.0.1/32 scram-sha-256
			hostnossl all      all  ::1/128      scram-sha-256
			hostnossl all      all  0.0.0.0/0    scram-sha-256
		EOF
	fi
}

setup_lega_users(){
	if [ -z "$LEGA_IN_PASSWORD" ]; then
		echo "Environment variable LEGA_IN_PASSWORD is empty. No password setting operation performed."
	elif [ -z "$LEGA_OUT_PASSWORD" ]; then
		echo "Environment variable LEGA_OUT_PASSWORD is empty. No password setting operation performed."
	else
		echo "Altering users passwords..."
		export PGPASSWORD="${PGPASSWORD:-$POSTGRES_PASSWORD}"
		temp_server_start
		sleep 2
		psql -v ON_ERROR_STOP=1 --username postgres --no-password --dbname "${POSTGRES_DB:-sda}" <<-EOSQL
			DO \$\$
			BEGIN
				IF (SELECT usename FROM pg_shadow WHERE usename = 'lega_in' AND passwd IS NULL) IS NOT NULL THEN
					ALTER USER lega_in WITH PASSWORD '$LEGA_IN_PASSWORD';
				END IF;

				IF (SELECT usename FROM pg_shadow WHERE usename = 'lega_out' AND passwd IS NULL) IS NOT NULL THEN
					ALTER USER lega_out WITH PASSWORD '$LEGA_OUT_PASSWORD';
				END IF;
			END
			\$\$;
		EOSQL
		pg_ctl -D "$PGDATA" -w stop
		unset PGPASSWORD
	fi
}
# Refuse to start on data we cannot use instead of silently running initdb
# next to it (upstream's docker-entrypoint.sh does the same).
if [ -s "$PGDATA/PG_VERSION" ]; then
	data_major="$(tr -d '[:space:]' <"$PGDATA/PG_VERSION")"
	if [ "$data_major" != "$PG_MAJOR" ]; then
		echo "Error: $PGDATA holds a PostgreSQL $data_major database, but this server is PostgreSQL $PG_MAJOR."
		echo "Upgrade the data (for example with pg_dump and restore, or pg_upgrade) before starting this image."
		exit 1
	fi
elif [ "$PGDATA" != "/var/lib/postgresql/$PG_MAJOR/docker" ] && [ -s "/var/lib/postgresql/$PG_MAJOR/docker/PG_VERSION" ]; then
	# our first PostgreSQL 18 images used upstream's default PGDATA
	echo "Error: PGDATA is $PGDATA, which is empty, but /var/lib/postgresql/$PG_MAJOR/docker holds a database."
	echo "Set PGDATA to /var/lib/postgresql/$PG_MAJOR/docker or move the data to $PGDATA."
	exit 1
elif [ "${PGDATA#/var/lib/postgresql/data/}" = "$PGDATA" ] && [ "$PGDATA" != /var/lib/postgresql/data ] && {
	[ -s /var/lib/postgresql/data/PG_VERSION ] ||
	# BusyBox mountpoint misses bind mounts, so also check mountinfo like upstream
	mountpoint -q /var/lib/postgresql/data ||
	awk '$5 == "/var/lib/postgresql/data" { found = 1 } END { exit !found }' /proc/self/mountinfo
}; then
	echo "Error: PGDATA is $PGDATA, which is empty and outside /var/lib/postgresql/data, but /var/lib/postgresql/data holds a database or is a mount point."
	echo "Mount the data volume at $PGDATA or set PGDATA to /var/lib/postgresql/data."
	exit 1
fi

# If already initialized, then run
if [ -s "$PGDATA/PG_VERSION" ]; then
	migrate "$@"

	setup_lega_users

	setup_hba_conf

	exec "$@"
fi

# Otherwise, do initialization (as postgres user)
if [ -z "$POSTGRES_PASSWORD" ]; then
	echo "You must specify POSTGRES_PASSWORD to a non-empty value for the superuser."
	exit 1
fi

initdb --username=postgres --pwfile=<(printf "%s\n" "$POSTGRES_PASSWORD") # no password: no authentication for postgres user

export PGPASSWORD="${PGPASSWORD:-$POSTGRES_PASSWORD}"
temp_server_start "$@"

# Create database
psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres --set db="${POSTGRES_DB:-sda}" <<-'EOSQL'
	SET TIME ZONE 'UTC';
	CREATE DATABASE :"db" ;
EOSQL

for f in docker-entrypoint-initdb.d/*; do
	echo "$0: running $f"
	echo
	psql -v ON_ERROR_STOP=1 --username postgres --dbname "${POSTGRES_DB:-sda}" -f "$f"
	echo
done

if [ -z "$LEGA_IN_PASSWORD" ]; then
	echo "Environment variable LEGA_IN_PASSWORD is empty. No password setting operation performed."
elif [ -z "$LEGA_OUT_PASSWORD" ]; then
	echo "Environment variable LEGA_OUT_PASSWORD is empty. No password setting operation performed."
else
	echo "Altering users passwords..."
	psql -v ON_ERROR_STOP=1 --username postgres --no-password --dbname "${POSTGRES_DB:-sda}" <<-EOSQL
		ALTER USER lega_in WITH PASSWORD '$LEGA_IN_PASSWORD';
		ALTER USER lega_out WITH PASSWORD '$LEGA_OUT_PASSWORD';
	EOSQL
fi

pg_ctl -D "$PGDATA" -m fast -w stop

unset PGPASSWORD

setup_hba_conf

echo
echo 'PostgreSQL init process complete; ready for start up.'
echo

exec "$@"
